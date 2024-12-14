package circuitbreaker

import (
	"context"
	"fmt"
	"loadbalancer/models"
	"net/http"
	"net/url"
	"sync"
	"time"
)

//go:generate mockgen -destination=mocks/mock_service.go -package=mocks loadbalancer/configurator ConfigService

type ConfigService interface {
	Subscribe() <-chan models.Config
}

type CircuitBreaker struct {
	config           models.Config
	configService    ConfigService
	healthCheck      func(server string) error
	availableServers map[string]bool
	m                sync.RWMutex
	parentCtx        context.Context
}

func NewCircuitBreaker(ctx context.Context, config models.Config, configService ConfigService) *CircuitBreaker {
	cb := &CircuitBreaker{
		config:           config,
		configService:    configService,
		availableServers: make(map[string]bool),
		parentCtx:        ctx,
	}

	cb.healthCheck = cb.defaultHealthCheck
	go cb.runHealthCheck(ctx)
	go cb.reloadConfig(ctx)
	return cb
}

func (cb *CircuitBreaker) defaultHealthCheck(serverID string) error {
	cb.m.RLock()
	defer cb.m.RUnlock()

	for _, group := range cb.config.TargetGroups {
		for _, server := range group.Servers {
			if server.ID == serverID {
				serverURL := fmt.Sprintf("http://%s:%d%s", server.Host, server.Port, server.HealthCheck)
				if _, err := url.Parse(serverURL); err != nil {
					return fmt.Errorf("invalid URL: %w", err)
				}
				req, err := http.NewRequestWithContext(cb.parentCtx, http.MethodGet, serverURL, nil)
				if err != nil {
					return fmt.Errorf("failed to create request: %w", err)
				}
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					return fmt.Errorf("request failed: %w", err)
				}
				if resp.StatusCode != http.StatusOK {
					return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
				}
				return nil
			}
		}
	}
	return fmt.Errorf("server ID not found")
}

func (cb *CircuitBreaker) reloadConfig(ctx context.Context) {
	configChan := cb.configService.Subscribe()
	for {
		select {
		case <-ctx.Done():
			return
		case newConfig := <-configChan:
			cb.m.Lock()
			cb.config = newConfig
			cb.availableServers = cb.CheckServers()
			cb.m.Unlock()
		}
	}
}

func (cb *CircuitBreaker) IsServerAlive(server string) (bool, error) {
	err := cb.healthCheck(server)
	if err == nil {
		return true, nil
	}
	return false, err
}

func (cb *CircuitBreaker) CheckServers() map[string]bool {
	cb.m.RLock()
	defer cb.m.RUnlock()

	results := make(map[string]bool)
	for _, group := range cb.config.TargetGroups {
		for _, server := range group.Servers {
			if server.Available {
				if isAlive, err := cb.IsServerAlive(server.ID); err == nil && isAlive {
					results[server.ID] = true
				}
			}
		}
	}
	return results
}

func (cb *CircuitBreaker) runHealthCheck(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cb.m.Lock()
			cb.availableServers = cb.CheckServers()
			cb.m.Unlock()
		}
	}
}

func (cb *CircuitBreaker) GetAvailableServersInGroup(targetGroupID string) []string {
	cb.m.RLock()
	defer cb.m.RUnlock()

	var servers []string
	for _, group := range cb.config.TargetGroups {
		if group.ID == targetGroupID {
			for _, server := range group.Servers {
				if isAlive, ok := cb.availableServers[server.ID]; ok && isAlive {
					servers = append(servers, server.ID)
				}
			}
			break
		}
	}
	return servers
}
