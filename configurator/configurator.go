package configurator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"loadbalancer/models"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

type Service struct {
	apiURL          url.URL
	config          atomic.Value
	subscribers     []chan models.Config
	subscribersLock sync.RWMutex
	client          *http.Client
}

// NewService initializes and returns a new Service
func NewService(apiURL url.URL) *Service {
	service := &Service{
		apiURL:      apiURL,
		subscribers: make([]chan models.Config, 0),
		client:      &http.Client{Timeout: 30 * time.Second},
	}

	service.config.Store(models.Config{}) // Store initial empty config

	return service
}

// LoadConfig fetches the configuration from the API
func (c *Service) LoadConfig(ctx context.Context) (models.Config, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.apiURL.String(), nil)
	if err != nil {
		return models.Config{}, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return models.Config{}, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return models.Config{}, fmt.Errorf("failed to fetch config: %s", resp.Status)
	}

	var cfg models.Config
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return models.Config{}, err
	}

	return cfg, nil
}

// ValidateConfig checks if the configuration is valid
var (
	ErrInvalidBalancingAlgorithm = errors.New("invalid balancing algorithm")
	ErrInvalidRoute              = errors.New("invalid route configuration")
	ErrInvalidTargetGroup        = errors.New("invalid target group configuration")
	ErrInvalidServer             = errors.New("invalid server configuration")
	ErrMissingTargetGroup        = errors.New("route references a non-existent target group")
)

func (c *Service) ValidateConfig(cfg models.Config) error {
	// Validate balancing configuration
	switch cfg.Balancing.Algorithm {
	case models.RoundRobin.String(), models.LeastConnection.String(), models.Weighted.String(), models.Random.String():
	default:
		return ErrInvalidBalancingAlgorithm
	}

	// Validate routes
	for _, route := range cfg.Routes {
		if route.PathPattern == "" || len(route.Methods) == 0 || route.TargetGroup == "" {
			return ErrInvalidRoute
		}
	}

	// Validate target groups and servers
	for _, tg := range cfg.TargetGroups {
		if tg.ID == "" || tg.Name == "" {
			return ErrInvalidTargetGroup
		}

		for _, server := range tg.Servers {
			if server.ID == "" || server.Name == "" || server.Host == "" || server.Port <= 0 {
				return ErrInvalidServer
			}

			serverURL := server.GetFullHealthCheck()
			if _, err := url.Parse(serverURL); err != nil {
				return fmt.Errorf("invalid server healthcheck URL: %s: %w", err, ErrInvalidServer)
			}
		}
	}

	// Ensure that all target groups in routes are present in the target groups list
	for _, route := range cfg.Routes {
		found := false
		for _, tg := range cfg.TargetGroups {
			if route.TargetGroup == tg.ID {
				found = true
				break
			}
		}
		if !found {
			return ErrMissingTargetGroup
		}
	}

	return nil
}

// ReloadConfig fetches, validates, and updates the configuration
func (c *Service) ReloadConfig(ctx context.Context) error {
	newConfig, err := c.LoadConfig(ctx)
	if err != nil {
		// Log or handle invalid config without updating
		return err
	}
	if err = c.ValidateConfig(newConfig); err != nil {
		// Log or handle invalid config without updating
		return err
	}

	c.config.Store(newConfig)
	c.notifySubscribers(newConfig)
	return nil
}

// GetConfig returns the current configuration safely
func (c *Service) GetConfig() (models.Config, error) {
	config, ok := c.config.Load().(models.Config)
	if !ok {
		return models.Config{}, fmt.Errorf("no configuration loaded")
	}
	return config, nil
}

// Subscribe adds a new subscriber channel for config updates
func (c *Service) Subscribe() <-chan models.Config {
	c.subscribersLock.Lock()
	defer c.subscribersLock.Unlock()

	ch := make(chan models.Config, 1)
	c.subscribers = append(c.subscribers, ch)
	return ch
}

// notifySubscribers sends the new configuration to all subscribers
func (c *Service) notifySubscribers(newConfig models.Config) {
	c.subscribersLock.RLock()
	defer c.subscribersLock.RUnlock()

	for _, subscriber := range c.subscribers {
		select {
		case subscriber <- newConfig:
		default:
		}
	}
}

// StartAutoReload sets up a timer to reload the configuration automatically
func (c *Service) StartAutoReload(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for {
			select {
			case <-ticker.C:
				err := c.ReloadConfig(ctx)
				if err != nil {
					fmt.Println("Error reloading config:", err)
					continue
				}
			case <-ctx.Done():
				ticker.Stop()
				return
			}
		}
	}()
}
