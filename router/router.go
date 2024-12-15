package router

import (
	"errors"
	"fmt"
	"loadbalancer/models"
	"sync/atomic"
)

type Service struct {
	config atomic.Value
}

func NewRouterService() *Service {
	s := &Service{}

	s.config.Store(models.Config{})

	return s
}

func (rs *Service) UpdateConfig(cfg models.Config) {
	rs.config.Store(cfg)
}

var ErrRouteNotFound = errors.New("no matching route found")

func (rs *Service) MatchRequest(req models.Request) (string, error) {
	config, ok := rs.config.Load().(models.Config)
	if !ok {
		return "", fmt.Errorf("no configuration loaded")
	}

	for _, route := range config.Routes {
		if route.PathPattern == req.Path &&
			contains(route.Methods, req.Method) &&
			matchHeaders(route.Headers, req.Headers) {
			return route.TargetGroup, nil
		}
	}

	return "", ErrRouteNotFound
}

func contains(slice []string, item string) bool {
	for _, val := range slice {
		if val == item {
			return true
		}
	}
	return false
}

func matchHeaders(routeHeaders, reqHeaders map[string][]string) bool {
	for key, values := range routeHeaders {
		reqValues, exists := reqHeaders[key]
		if !exists || !containsAll(reqValues, values) {
			return false
		}
	}
	return true
}

func containsAll(slice, subset []string) bool {
	set := make(map[string]struct{}, len(slice))
	for _, s := range slice {
		set[s] = struct{}{}
	}
	for _, s := range subset {
		if _, found := set[s]; !found {
			return false
		}
	}
	return true
}
