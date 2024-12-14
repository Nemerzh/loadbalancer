package models

type Algorithm string

func (a Algorithm) String() string {
	return string(a)
}

const (
	RoundRobin      Algorithm = "round_robin"
	LeastConnection Algorithm = "least_connection"
	Weighted        Algorithm = "weighted"
)

type Server struct {
	ID          string
	Name        string
	Host        string
	Port        int
	Weight      int
	Available   bool
	HealthCheck string
}

type TargetGroup struct {
	ID      string
	Name    string
	Servers []Server
}

type Route struct {
	PathPattern string
	Methods     []string
	Headers     map[string][]string
	TargetGroup string
}

type BalancerConfig struct {
	Algorithm string
}

type Config struct {
	Balancing    BalancerConfig
	Routes       []Route
	TargetGroups []TargetGroup
}

type Request struct {
	Path    string
	Method  string
	Headers map[string][]string
}
