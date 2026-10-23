package web

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
)

// HTTP metrics exposed at /metrics via promhttp.
var (
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests.",
		},
		[]string{"method", "path", "status"},
	)
	httpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request latency in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)
	httpRequestsInFlight = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "http_requests_in_flight",
			Help: "Current number of in-flight HTTP requests.",
		},
	)
)

func init() {
	prometheus.MustRegister(httpRequestsTotal, httpRequestDuration, httpRequestsInFlight)
}

// unmatchedPath is the path label for requests that matched no route.
const unmatchedPath = "unmatched"

// Metrics records Prometheus metrics for every request. The "path" label uses
// the route pattern (e.g. /api/v1/users/:id) instead of raw paths so the
// label cardinality stays bounded.
func Metrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		httpRequestsInFlight.Inc()
		defer httpRequestsInFlight.Dec()

		start := time.Now()
		c.Next()

		// Unknown routes have no pattern; a fixed label keeps cardinality
		// bounded (raw paths from scanners would explode the metric).
		path := c.FullPath()
		if path == "" {
			path = unmatchedPath
		}
		status := strconv.Itoa(c.Writer.Status())
		httpRequestsTotal.WithLabelValues(c.Request.Method, path, status).Inc()
		httpRequestDuration.WithLabelValues(c.Request.Method, path).Observe(time.Since(start).Seconds())
	}
}
