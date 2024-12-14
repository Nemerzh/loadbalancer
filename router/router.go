package router

import (
	"fmt"
	"loadbalancer/models"
)

type RouterService struct {
	config models.Config
}

func NewRouterService(config models.Config) *RouterService {
	return &RouterService{config: config}
}

func (rs *RouterService) MatchRequest(req models.Request) (string, error) {
	for _, route := range rs.config.Routes {
		if route.PathPattern == req.Path &&
			contains(route.Methods, req.Method) &&
			matchHeaders(route.Headers, req.Headers) {
			return route.TargetGroup, nil
		}
	}
	return "", fmt.Errorf("no matching route found")
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
