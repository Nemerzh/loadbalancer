package main

import (
	"context"
	"encoding/json"
	"io"
	"loadbalancer/balancer"
	"loadbalancer/circuitbreaker"
	"loadbalancer/configurator"
	"loadbalancer/models"
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

	wg := sync.WaitGroup{}
	wg.Add(3)

	// Start server 1
	go func() {
		defer wg.Done()

		mux := http.NewServeMux()
		mux.HandleFunc("/data", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			response := map[string]string{"message": "Hello from Server 1"}
			json.NewEncoder(w).Encode(response)
		})
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			response := map[string]string{"app": "OK", "name": "Server 1"}
			json.NewEncoder(w).Encode(response)

			log.Printf("got health request for the Server 1")
		})
		log.Println("Server 1 running on port 8081")
		log.Fatal(http.ListenAndServe(":8081", mux))
	}()

	// Start server 2
	go func() {
		defer wg.Done()
		mux := http.NewServeMux()
		mux.HandleFunc("/data", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			response := map[string]string{"message": "Hello from Server 2"}
			json.NewEncoder(w).Encode(response)
		})
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			response := map[string]string{"app": "OK", "name": "Server 2"}
			json.NewEncoder(w).Encode(response)

			log.Printf("got health request for the Server 2")
		})
		log.Println("Server 2 running on port 8082")
		log.Fatal(http.ListenAndServe(":8082", mux))
	}()

	// Start server 3
	go func() {
		defer wg.Done()

		mux := http.NewServeMux()
		mux.HandleFunc("/data", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			response := map[string]string{"message": "Hello from Server 3"}
			json.NewEncoder(w).Encode(response)
		})
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			log.Printf("got health request for the Server 3")
			w.Header().Set("Content-Type", "application/json")
			response := map[string]string{"app": "OK", "name": "Server 3"}
			json.NewEncoder(w).Encode(response)

		})
		log.Println("Server 3 running on port 8084")
		log.Fatal(http.ListenAndServe(":8084", mux))
	}()

	// Start server 3 with config response
	go func() {
		defer wg.Done()
		isCalled := false
		mux := http.NewServeMux()
		mux.HandleFunc("/config", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			config := models.Config{
				Balancing: models.BalancerConfig{
					Algorithm: "weighted",
				},
				Routes: []models.Route{
					{
						PathPattern: "/data",
						Methods:     []string{"GET"},
						Headers:     map[string][]string{},
						TargetGroup: "group-1",
					},
				},
				TargetGroups: []models.TargetGroup{
					{
						ID:   "group-1",
						Name: "Primary Group",
						Servers: []models.Server{
							{
								ID:          "server-1",
								Name:        "Server One",
								Scheme:      "http",
								Host:        "localhost",
								Port:        8081,
								Weight:      2,
								Available:   true,
								HealthCheck: "/health",
							},
							{
								ID:          "server-2",
								Name:        "Server Two",
								Scheme:      "http",
								Host:        "localhost",
								Port:        8082,
								Weight:      3,
								Available:   true,
								HealthCheck: "/health",
							},
							{
								ID:          "server-3",
								Name:        "Server Three",
								Scheme:      "http",
								Host:        "localhost",
								Port:        8084,
								Weight:      1,
								Available:   true,
								HealthCheck: "/health",
							},
						},
					},
				},
			}

			if isCalled {
				config.Balancing.Algorithm = "least_connection"
			}

			err := json.NewEncoder(w).Encode(config)
			if err != nil {
				log.Printf("got the error %s while encode a config", err)
				return
			}

			log.Println("config was called")
		})
		log.Println("Server 3 running on port 8083")
		log.Fatal(http.ListenAndServe(":8083", mux))
	}()

	parentCtx := context.Background()

	cfgManager := configurator.NewService(url.URL{
		Scheme: "http",
		Host:   "127.0.0.1:8083",
		Path:   "config",
	})

	lb := balancer.NewLoadBalancer(
		parentCtx,
		circuitbreaker.NewCircuitBreaker(parentCtx, cfgManager),
		router.NewRouterService(),
		cfgManager,
	)
	log.Printf("load balancer started at port :8090")
	go func() {
		log.Fatal(http.ListenAndServe(":8090", lb))
	}()

	cfgManager.StartAutoReload(parentCtx, 30*time.Second)

	time.AfterFunc(time.Second*1, func() {
		err := cfgManager.ReloadConfig(parentCtx)
		if err != nil {
			log.Printf("got the error %s while reloading a config", err)
			return
		}

		for i := 0; i < 100; i++ {
			time.Sleep(1 * time.Second)
			func() {
				r, _ := http.Get("http://127.0.0.1:8090/data")
				b, _ := io.ReadAll(r.Body)
				log.Printf("got %d resp: %s", i, string(b))
			}()
		}
	})

	// Wait for all servers to complete
	wg.Wait()
}
