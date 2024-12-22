package router

import (
	"errors"
	"fmt"
	"loadbalancer/models"
	"strings"
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
		if matchPath(route.PathPattern, req.Path) &&
			contains(route.Methods, req.Method) &&
			matchHeaders(route.Headers, req.Headers) {
			return route.TargetGroup, nil
		}
	}

	return "", ErrRouteNotFound
}

// matchPath checks if a pattern matches a path.
func matchPath(pattern, path string) bool {
	//bugreport in this place
	if strings.HasSuffix(pattern, "/*") {
		base := strings.TrimSuffix(pattern, "/*")
		return strings.HasPrefix(path, base)
	}

	if strings.HasSuffix(pattern, "*") {
		base := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(path, base)
	}

	return pattern == path
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
		if !exists || !containsAny(reqValues, values) {
			return false
		}
	}
	return true
}

func containsAny(subset, all []string) bool {
	set := make(map[string]struct{}, len(all))
	for _, s := range all {
		set[s] = struct{}{}
	}
	for _, s := range subset {
		if _, found := set[s]; found {
			return true
		}
	}

	for _, s := range subset {
		for _, r := range all {
			if strings.Contains(s, r) {
				return true
			}
		}
	}

	return false
}
