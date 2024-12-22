package balancer

import (
	"context"
	"errors"
	"fmt"
	"loadbalancer/models"
	"loadbalancer/router"
	"log"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"
)

type Router interface {
	MatchRequest(req models.Request) (string, error)
	UpdateConfig(cfg models.Config)
}

type CircuitBreaker interface {
	GetAvailableServersInGroup(targetGroupID string) []string
}

type ConfigService interface {
	Subscribe() <-chan models.Config
}

type MetricsWrapper interface {
	WrapHandlerForProxy(proxy string, addr string, handler http.Handler) http.Handler
}

type LoadBalancer struct {
	config          models.Config
	configLock      sync.RWMutex
	proxies         map[string][]*Proxy
	proxiesLock     sync.RWMutex
	latestProxy     string
	latestProxyLock sync.RWMutex
	reqCount        int
	reqCountLock    sync.RWMutex
	circuitBreaker  CircuitBreaker
	router          Router
	configService   ConfigService
	metricsWrapper  MetricsWrapper
	parentCtx       context.Context
}

func NewLoadBalancer(ctx context.Context, cb CircuitBreaker, rt Router, cs ConfigService, metricsWrapper MetricsWrapper) *LoadBalancer {
	lb := &LoadBalancer{
		circuitBreaker: cb,
		router:         rt,
		configService:  cs,
		metricsWrapper: metricsWrapper,
		parentCtx:      ctx,
		proxies:        make(map[string][]*Proxy),
	}

	go lb.configWatcher(ctx)

	return lb
}

func (lb *LoadBalancer) configWatcher(ctx context.Context) {
	configChan := lb.configService.Subscribe()
	for {
		select {
		case <-ctx.Done():
			return
		case newConfig := <-configChan:

			proxies := make(map[string][]*Proxy, len(newConfig.TargetGroups))
			for _, g := range newConfig.TargetGroups {
				proxies[g.ID] = make([]*Proxy, 0, len(g.Servers))

				for _, s := range g.Servers {
					if !s.Available {
						continue
					}

					proxies[g.ID] = append(proxies[g.ID], NewProxy(s.ID, s.GetBaseURL(), s.Weight))
				}
			}

			lb.configLock.Lock()
			lb.config = newConfig
			lb.configLock.Unlock()

			lb.router.UpdateConfig(newConfig)

			lb.proxiesLock.Lock()
			for g, p := range proxies {
				oldProxies, found := lb.proxies[g]
				if !found {
					lb.proxies[g] = p

					continue
				}

				mergedProxies := make([]*Proxy, 0)
				for _, newP := range p {
					var isOldExists bool

					for _, oldP := range oldProxies {
						if oldP.id == newP.id {
							mergedProxies = append(mergedProxies, oldP)
							isOldExists = true

							break
						}
					}

					if !isOldExists {
						mergedProxies = append(mergedProxies, newP)
					}
				}

				lb.proxies[g] = mergedProxies
			}
			lb.proxiesLock.Unlock()
		}
	}
}

var ErrNoServersAvailable = errors.New("no available server can process the request")

func (lb *LoadBalancer) GetProxy(req models.Request) (*Proxy, error) {
	target, err := lb.router.MatchRequest(req)
	if err != nil {
		return nil, fmt.Errorf("failed to match request: %w", err)
	}

	servers := lb.circuitBreaker.GetAvailableServersInGroup(target)
	if len(servers) == 0 {
		return nil, ErrNoServersAvailable
	}

	lb.proxiesLock.RLock()
	defer lb.proxiesLock.RUnlock()

	targetProxies, found := lb.proxies[target]
	if !found {
		return nil, ErrNoServersAvailable
	}

	availableProxies := make([]*Proxy, 0, len(targetProxies))

	for _, p := range targetProxies {
		for _, s := range servers {
			if s == p.id {
				availableProxies = append(availableProxies, p)
			}
		}
	}

	lb.configLock.RLock()
	alg := lb.config.Balancing.Algorithm
	lb.configLock.RUnlock()

	lb.reqCountLock.RLock()
	count := lb.reqCount
	lb.reqCountLock.RUnlock()

	lb.latestProxyLock.RLock()
	lastProxy := lb.latestProxy
	lb.latestProxyLock.RUnlock()

	var p *Proxy
	var last string
	switch alg {
	case models.RoundRobin.String():
		p, last = ApplyRoundRobin(availableProxies, lastProxy, count)
	case models.LeastConnection.String():
		p, last = ApplyLeastConnection(availableProxies, lastProxy, count)
	case models.Weighted.String():
		p, last = ApplyWeighted(availableProxies, lastProxy, count)
	case models.Random.String():
		p, last = ApplyRandom(availableProxies, lastProxy, count)
	default:
		return nil, ErrNoServersAvailable
	}

	if p == nil || last == "" {
		return nil, ErrNoServersAvailable
	}

	return p, nil
}

func fillRequestFromHTTP(r *http.Request) models.Request {
	// Copy headers from http.Request to the custom Request type
	headers := make(map[string][]string)
	for key, values := range r.Header {
		// Copy the header values
		headers[key] = append([]string(nil), values...)
	}

	return models.Request{
		Path:    r.URL.Path,
		Method:  r.Method,
		Headers: headers,
	}
}

func (lb *LoadBalancer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req := fillRequestFromHTTP(r)
	p, err := lb.GetProxy(req)
	if err != nil {
		if errors.Is(err, router.ErrRouteNotFound) {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("Route not found"))
			return
		}

		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("The server didn't respond"))
		return
	}

	isTheSame := false
	lb.latestProxyLock.Lock()
	if lb.latestProxy == p.id {
		isTheSame = true
	}
	lb.latestProxy = p.id
	lb.latestProxyLock.Unlock()

	lb.reqCountLock.Lock()
	if isTheSame {
		lb.reqCount++
	} else {
		lb.reqCount = 1
	}
	lb.reqCountLock.Unlock()

	wrappedHandler := lb.metricsWrapper.WrapHandlerForProxy(p.id, p.addr.String(), p)
	wrappedHandler.ServeHTTP(w, r)
}

// Implementations of each algorithm
func ApplyRoundRobin(servers []*Proxy, lastProxyID string, reqCount int) (*Proxy, string) {
	if len(servers) == 0 {
		return nil, ""
	}

	lastIndex := 0
	if lastProxyID == "" {
		return servers[0], servers[0].id
	}

	for i, p := range servers {
		if p.id == lastProxyID {
			lastIndex = i

			break
		}
	}

	nextIndex := (lastIndex + 1) % len(servers)

	return servers[nextIndex], servers[nextIndex].id
}

func ApplyLeastConnection(servers []*Proxy, lastProxyID string, reqCount int) (*Proxy, string) {
	if len(servers) == 0 {
		return nil, ""
	}

	var leastConnectedPeer *Proxy
	// Find at least one valid peer
	for _, b := range servers {
		leastConnectedPeer = b
		break
	}

	// Check which one has the least number of active connections
	for _, b := range servers {
		if leastConnectedPeer.GetLoad() > b.GetLoad() {
			leastConnectedPeer = b
		}
	}

	if leastConnectedPeer == nil {
		return nil, ""
	}

	return leastConnectedPeer, leastConnectedPeer.id
}

func ApplyWeighted(servers []*Proxy, lastProxyID string, reqCount int) (*Proxy, string) {
	if len(servers) == 0 {
		return nil, ""
	}

	lastIndex := 0
	if lastProxyID == "" {
		return servers[0], servers[0].id
	}

	for i, p := range servers {
		if p.id != lastProxyID {
			continue
		}

		lastIndex = i

		if reqCount >= servers[lastIndex].weight {
			break
		}

		return servers[lastIndex], servers[lastIndex].id
	}

	nextIndex := (lastIndex + 1) % len(servers)

	return servers[nextIndex], servers[nextIndex].id
}

func ApplyRandom(servers []*Proxy, lastProxyID string, reqCount int) (*Proxy, string) {
	if len(servers) == 0 {
		return nil, ""
	}

	nextIndex := rand.Intn(len(servers))

	return servers[nextIndex], servers[nextIndex].id
}

// NewProxy is the Proxy constructor
func NewProxy(name string, addr *url.URL, weight int) *Proxy {
	return &Proxy{
		id:     name,
		proxy:  httputil.NewSingleHostReverseProxy(addr),
		weight: weight,
		addr:   addr,
	}
}

// Proxy is a simple http proxy entity
type Proxy struct {
	id      string
	addr    *url.URL
	proxy   *httputil.ReverseProxy
	isAlive bool
	load    int32
	weight  int
}

// ServeHTTP proxies incoming requests
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt32(&p.load, 1)
	defer func() {
		atomic.AddInt32(&p.load, -1)
		log.Printf("request %s was handled by server %s (%s), current load: %d",
			r.URL.String(), p.id, p.addr, p.GetLoad())
	}()

	log.Printf("request %s will be forwarded to server %s (%s), current load: %d",
		r.URL.String(), p.id, p.addr, p.GetLoad())

	p.proxy.ServeHTTP(w, r)

}

// GetLoad returns the number of requests being served by the proxy at the moment
func (p *Proxy) GetLoad() int32 {
	return atomic.LoadInt32(&p.load)
}
