package circuitbreaker

import (
	"context"
	"fmt"
	"loadbalancer/models"
	"log"
	"net/http"
	"sync"
	"time"
)

//go:generate mockgen -destination=mocks/mock_service.go -package=mocks loadbalancer/configurator ConfigService

type ConfigService interface {
	Subscribe() <-chan models.Config
}

type serverState struct {
	isAlive   bool
	err       error
	checkedAt time.Time
}

type CircuitBreaker struct {
	config               models.Config
	configLock           sync.RWMutex
	configService        ConfigService
	availableServers     map[string]serverState
	availableServersLock sync.RWMutex
	parentCtx            context.Context
	client               *http.Client
}

func NewCircuitBreaker(ctx context.Context, configService ConfigService) *CircuitBreaker {
	cb := &CircuitBreaker{
		config:           models.Config{},
		configService:    configService,
		availableServers: make(map[string]serverState),
		parentCtx:        ctx,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}

	go cb.runHealthCheck(ctx)
	go cb.reloadConfig(ctx)

	return cb
}

func (cb *CircuitBreaker) defaultHealthCheck(serverHealthcheckURL string) error {
	req, err := http.NewRequestWithContext(cb.parentCtx, http.MethodGet, serverHealthcheckURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := cb.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}

	err = resp.Body.Close()
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

func (cb *CircuitBreaker) reloadConfig(ctx context.Context) {
	configChan := cb.configService.Subscribe()
	for {
		select {
		case <-ctx.Done():
			return
		case newConfig := <-configChan:
			cb.availableServersLock.Lock()
			cb.availableServers = cb.CheckServers(newConfig.TargetGroups)
			cb.availableServersLock.Unlock()

			cb.configLock.Lock()
			cb.config = newConfig
			cb.configLock.Unlock()
		}
	}
}

func (cb *CircuitBreaker) getServerState(serverHealthcheck string) serverState {
	err := cb.defaultHealthCheck(serverHealthcheck)
	log.Printf("got err %+v for %s ", err, serverHealthcheck)

	return serverState{
		isAlive:   err == nil,
		err:       err,
		checkedAt: time.Now(),
	}
}

func (cb *CircuitBreaker) CheckServers(groups []models.TargetGroup) map[string]serverState {
	results := make(map[string]serverState)
	for _, group := range groups {
		for _, server := range group.Servers {
			if !server.Available {
				continue
			}

			results[server.ID] = cb.getServerState(server.GetFullHealthCheck())
		}
	}

	return results
}

func (cb *CircuitBreaker) runHealthCheck(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cb.configLock.RLock()
			groups := cb.config.TargetGroups
			cb.configLock.RUnlock()

			cb.availableServersLock.Lock()
			cb.availableServers = cb.CheckServers(groups)
			cb.availableServersLock.Unlock()
		}
	}
}

func (cb *CircuitBreaker) GetAvailableServersInGroup(targetGroupID string) []string {
	cb.availableServersLock.RLock()
	defer cb.availableServersLock.RUnlock()

	var servers []string
	for _, group := range cb.config.TargetGroups {
		if group.ID != targetGroupID {
			continue
		}

		for _, server := range group.Servers {
			if state := cb.availableServers[server.ID]; state.isAlive {
				servers = append(servers, server.ID)
			}
		}

		return servers
	}

	return servers
}
