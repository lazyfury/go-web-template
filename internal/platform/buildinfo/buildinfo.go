// Package buildinfo exposes the running binary's identity as a Prometheus
// metric. Version and commit otherwise only appear in the startup log line,
// which makes "which build is this alert firing on?" unanswerable after the
// fact (and joins/dashboards cannot label by log lines).
package buildinfo

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

// Register exposes build_info{version,commit} = 1 on reg. It is idempotent and
// updates the labels when a second registration happens (tests rebuild the app
// in one process).
func Register(reg prometheus.Registerer, version, commit string) error {
	gauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "build_info",
		Help: "Build information; the value is always 1.",
	}, []string{"version", "commit"})

	if err := reg.Register(gauge); err != nil {
		var already prometheus.AlreadyRegisteredError
		if !errors.As(err, &already) {
			return err
		}
		existing, ok := already.ExistingCollector.(*prometheus.GaugeVec)
		if !ok {
			return nil
		}
		gauge = existing
	}
	gauge.WithLabelValues(version, commit).Set(1)
	return nil
}
