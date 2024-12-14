package balancer

import (
	"context"
	"errors"
	"fmt"
	"loadbalancer/models"
	"sync"
)

type Router interface {
	MatchRequest(req models.Request) (string, error)
}

type CircuitBreaker interface {
	GetAvailableServers() []string
}

type ConfigService interface {
	Subscribe() <-chan models.Config
}

type LoadBalancer struct {
	config         models.Config
	circuitBreaker CircuitBreaker
	router         Router
	configService  ConfigService
	mu             sync.RWMutex
	parentCtx      context.Context
}

func NewLoadBalancer(ctx context.Context, cb CircuitBreaker, rt Router, cs ConfigService) *LoadBalancer {
	lb := &LoadBalancer{
		circuitBreaker: cb,
		router:         rt,
		configService:  cs,
		parentCtx:      ctx,
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
			lb.mu.Lock()
			lb.config = newConfig
			lb.mu.Unlock()
		}
	}
}

func (lb *LoadBalancer) SelectTarget(ctx context.Context, req models.Request) (string, error) {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	target, err := lb.router.MatchRequest(req)
	if err != nil {
		return "", fmt.Errorf("failed to match request: %w", err)
	}

	servers := lb.circuitBreaker.GetAvailableServers()
	for _, server := range servers {
		if server == target {
			return server, nil
		}
	}
	return "", errors.New("no available server can process the request")
}

type Algorithm string

const (
	RoundRobin      Algorithm = "round_robin"
	LeastConnection Algorithm = "least_connection"
	Weighted        Algorithm = "weighted"
)

// Implementations of each algorithm
func ApplyRoundRobin(servers []string, lastIndex int) (string, int) {
	if len(servers) == 0 {
		return "", -1
	}
	nextIndex := (lastIndex + 1) % len(servers)
	return servers[nextIndex], nextIndex
}

func ApplyLeastConnection(servers map[string]int) string {
	var target string
	minConnections := int(^uint(0) >> 1) // max int value
	for server, connections := range servers {
		if connections < minConnections {
			target = server
			minConnections = connections
		}
	}
	return target
}

func ApplyWeighted(servers map[string]int, weights map[string]int) string {
	var target string
	maxWeight := -1
	for server, weight := range weights {
		if srvWeight := servers[server] + weight; srvWeight > maxWeight {
			target = server
			maxWeight = srvWeight
		}
	}
	return target
}
