package housekeeping

import "github.com/prometheus/client_golang/prometheus"

// Retention metrics: how much each task deletes, and whether it is failing.
// Without these a silently broken job only shows up as a full disk.
var (
	purgedRowsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "housekeeping_purged_rows_total",
			Help: "Rows hard-deleted by the retention janitor, by task.",
		},
		[]string{"task"},
	)
	taskFailuresTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "housekeeping_task_failures_total",
			Help: "Failed retention runs, by task.",
		},
		[]string{"task"},
	)
)

func init() {
	prometheus.MustRegister(purgedRowsTotal, taskFailuresTotal)
}
