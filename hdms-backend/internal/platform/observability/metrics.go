package observability

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// MetricsHandler serves /metrics in Prometheus text format using the
// default Go/process collectors registered by the promhttp package.
func MetricsHandler() http.Handler {
	return promhttp.Handler()
}
