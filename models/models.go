package models

import (
	"fmt"
	"net/url"
	"strings"
)

type Algorithm string

func (a Algorithm) String() string {
	return string(a)
}

const (
	RoundRobin      Algorithm = "ROUND_ROBIN"
	LeastConnection Algorithm = "LEAST_CONNECTIONS"
	Weighted        Algorithm = "WEIGHTED"
	Random          Algorithm = "random"
)

type Config struct {
	Balancing    BalancerConfig `json:"balancing"`
	Routes       []Route        `json:"routes"`
	TargetGroups []TargetGroup  `json:"target_groups"`
}

type TargetGroup struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Servers []Server `json:"servers"`
}

type Route struct {
	PathPattern string              `json:"path_pattern"`
	Methods     []string            `json:"methods"`
	Headers     map[string][]string `json:"headers"`
	TargetGroup string              `json:"target_group"`
}

type BalancerConfig struct {
	Algorithm string `json:"algorithm"`
}

type Server struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Scheme      string `json:"scheme"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Weight      int    `json:"weight"`
	Available   bool   `json:"available"`
	HealthCheck string `json:"health_check"`
}

func (s Server) GetBaseURL() *url.URL {
	baseURL := url.URL{
		Scheme: s.Scheme,
		Host:   fmt.Sprintf("%s:%d", s.Host, s.Port),
	}

	return &baseURL
}

func (s Server) GetFullHealthCheck() string {
	healthCheckURL := url.URL{
		Scheme: s.Scheme,
		Host:   fmt.Sprintf("%s:%d", s.Host, s.Port),
		Path:   strings.TrimSuffix(s.HealthCheck, "/"),
	}

	return healthCheckURL.String()
}

type Request struct {
	Path    string
	Method  string
	Headers map[string][]string
}
