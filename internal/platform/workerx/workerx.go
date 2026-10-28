// Package workerx holds the metrics every background worker shares.
//
// ADR 14 says workers live in their own package and are started by cmd/server.
// Liveness is the same question for all of them ("did a full pass complete
// recently?"), so the metric lives here instead of being reinvented per domain:
// counters (rows purged, deliveries sent) cannot distinguish "nothing to do"
// from "the loop is stuck".
package workerx

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Worker label values. Keep them stable: deploy/prometheus/alerts.yml refers to
// them by name.
const (
	MessageDelivery = "message_delivery"
	Housekeeping    = "housekeeping"
)

var lastSuccess = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Name: "worker_last_success_timestamp_seconds",
	Help: "Unix timestamp of the last completed worker pass (0/absent = never completed).",
}, []string{"worker"})

func init() {
	prometheus.MustRegister(lastSuccess)
}

// MarkSuccess records that worker finished a full pass at t.
func MarkSuccess(worker string, t time.Time) {
	lastSuccess.WithLabelValues(worker).Set(float64(t.Unix()))
}
