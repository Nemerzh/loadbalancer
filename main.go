package main

import (
	"context"
	"encoding/json"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"loadbalancer/balancer"
	"loadbalancer/circuitbreaker"
	"loadbalancer/configurator"
	"loadbalancer/metricscollector"
	"loadbalancer/router"
	"log"
	"net/http"
	"net/url"
	"runtime"
	"sync"
	"time"
)

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	parentCtx := context.Background()

	cfgManager := configurator.NewService(url.URL{
		Scheme: "http",
		Host:   "127.0.0.1:8000",
		Path:   "configuration",
	})

	metrics := metricscollector.NewMetricsCollector()

	lb := balancer.NewLoadBalancer(
		parentCtx,
		circuitbreaker.NewCircuitBreaker(parentCtx, cfgManager, 2*time.Second),
		router.NewRouterService(),
		cfgManager,
		metrics,
	)

	err := cfgManager.LoadConfig(parentCtx)
	if err != nil {
		log.Fatalf("can't load config %s", err)

		return
	}

	wg := sync.WaitGroup{}
	wg.Add(1)

	go func() {
		configMux := http.NewServeMux()
		configMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/reload_config":
				if r.Method != http.MethodGet {
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}

				err := cfgManager.LoadConfig(r.Context())
				if err != nil {
					w.WriteHeader(http.StatusBadGateway)
					w.Write([]byte("The server didn't handle this request correctly"))
					return
				}

				w.WriteHeader(http.StatusOK)
				w.Write([]byte("Config has been reloaded"))
				return
			case "/api/config":
				if r.Method != http.MethodGet {
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}

				cfg, err := cfgManager.GetConfig()
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte(err.Error()))
					return
				}

				w.WriteHeader(http.StatusOK)

				if err = json.NewEncoder(w).Encode(cfg); err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte(err.Error()))
					return
				}
			default:
				w.WriteHeader(http.StatusNotFound)
			}

		})

		log.Printf("load balancer API started at port :8101")
		log.Fatal(http.ListenAndServe(":8101", configMux))
	}()

	mux := http.NewServeMux()

	mux.Handle("/", metrics.WrapHandler(lb))

	// Expose metrics endpoint
	mux.Handle("/metrics", promhttp.Handler())

	log.Printf("load balancer started at port :8100")
	log.Fatal(http.ListenAndServe(":8100", mux))

	wg.Wait()
}
