package metricscollector

import (
	"github.com/prometheus/client_golang/prometheus"
	"net/http"
	"strconv"
	"time"
)

type MetricsCollector struct {
	requestCount           *prometheus.CounterVec
	requestDuration        *prometheus.HistogramVec
	requestCountByProxy    *prometheus.CounterVec
	requestDurationByProxy *prometheus.HistogramVec
}

// NewMetricsCollector initializes and registers Prometheus metrics.
func NewMetricsCollector() *MetricsCollector {
	mc := &MetricsCollector{
		requestCount: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "http_requests_total",
				Help: "Total number of HTTP requests",
			},
			[]string{"method", "path", "status"},
		),
		requestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "http_request_duration_seconds",
				Help:    "Histogram of request durations",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method", "path"},
		),
		requestCountByProxy: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "proxy_http_requests_total",
				Help: "Total number of requests handled by the proxy",
			},
			[]string{"method", "path", "status", "proxy", "proxy_addr"},
		),
		requestDurationByProxy: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "proxy_http_request_duration_seconds",
				Help:    "Time taken to handle requests by proxy",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method", "path", "status", "proxy", "proxy_addr"},
		),
	}

	prometheus.MustRegister(mc.requestCount)
	prometheus.MustRegister(mc.requestDuration)
	prometheus.MustRegister(mc.requestCountByProxy)
	prometheus.MustRegister(mc.requestDurationByProxy)

	return mc
}

// WrapHandler wraps an HTTP handler to collect metrics.
func (mc *MetricsCollector) WrapHandler(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()

		// Wrap the ResponseWriter to capture status code
		metricsWriter := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		handler.ServeHTTP(metricsWriter, r)

		duration := time.Since(startTime).Seconds()
		mc.requestCount.WithLabelValues(r.Method, r.URL.Path, strconv.Itoa(metricsWriter.statusCode)).Inc()
		mc.requestDuration.WithLabelValues(r.Method, r.URL.Path).Observe(duration)
	})
}

// WrapHandlerForProxy wraps an HTTP handler to collect metrics.
func (mc *MetricsCollector) WrapHandlerForProxy(proxy string, addr string, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()

		// Wrap the ResponseWriter to capture status code
		metricsWriter := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		handler.ServeHTTP(metricsWriter, r)

		duration := time.Since(startTime).Seconds()
		mc.requestCountByProxy.WithLabelValues(r.Method, r.URL.Path, strconv.Itoa(metricsWriter.statusCode), proxy, addr).Inc()
		mc.requestDurationByProxy.WithLabelValues(r.Method, r.URL.Path, strconv.Itoa(metricsWriter.statusCode), proxy, addr).Observe(duration)
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

// WriteHeader captures the status code.
func (rw *responseWriter) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
	rw.ResponseWriter.WriteHeader(statusCode)
}
