package message

import "github.com/prometheus/client_golang/prometheus"

// Delivery metrics. `result` is a closed set, and the only other label is the
// channel, so a per-recipient or per-target label can never leak into
// Prometheus (see design-message §11: no PII in metrics).
var (
	deliveryAttemptsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "message_delivery_attempts_total",
			Help: "External delivery attempts by channel and result (sent, failed, dead, no_sender, lease_lost).",
		},
		[]string{"channel", "result"},
	)
	deliveryDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "message_delivery_duration_seconds",
			Help:    "Duration of one external send, by channel.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"channel"},
	)
)

func init() {
	prometheus.MustRegister(deliveryAttemptsTotal, deliveryDuration)
}
