package dbx

import (
	"database/sql"
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

// poolCollector exposes database/sql pool statistics. Without them a saturated
// pool looks like "everything is slow": requests queue in the driver while the
// HTTP latency panel blames the handlers.
type poolCollector struct {
	db    *sql.DB
	descs []*prometheus.Desc
}

func newPoolCollector(db *sql.DB) *poolCollector {
	descs := []*prometheus.Desc{
		prometheus.NewDesc("db_pool_open_connections", "Open connections, in use or idle.", nil, nil),
		prometheus.NewDesc("db_pool_in_use_connections", "Connections currently used by a request.", nil, nil),
		prometheus.NewDesc("db_pool_idle_connections", "Idle connections in the pool.", nil, nil),
		prometheus.NewDesc("db_pool_max_open_connections", "Configured upper bound on open connections (0 = unlimited).", nil, nil),
		prometheus.NewDesc("db_pool_wait_count_total", "Total waits because every connection was busy.", nil, nil),
		prometheus.NewDesc("db_pool_wait_duration_seconds_total", "Total time blocked waiting for a connection.", nil, nil),
		prometheus.NewDesc("db_pool_max_idle_closed_total", "Connections closed because they exceeded max idle.", nil, nil),
		prometheus.NewDesc("db_pool_max_lifetime_closed_total", "Connections closed because they exceeded max lifetime.", nil, nil),
	}
	return &poolCollector{db: db, descs: descs}
}

// Describe implements prometheus.Collector.
func (c *poolCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range c.descs {
		ch <- desc
	}
}

// Collect implements prometheus.Collector.
func (c *poolCollector) Collect(ch chan<- prometheus.Metric) {
	stats := c.db.Stats()

	gauge := func(desc *prometheus.Desc, value float64) {
		ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, value)
	}
	counter := func(desc *prometheus.Desc, value float64) {
		ch <- prometheus.MustNewConstMetric(desc, prometheus.CounterValue, value)
	}

	d := c.descs
	gauge(d[0], float64(stats.OpenConnections))
	gauge(d[1], float64(stats.InUse))
	gauge(d[2], float64(stats.Idle))
	gauge(d[3], float64(stats.MaxOpenConnections))
	counter(d[4], float64(stats.WaitCount))
	counter(d[5], stats.WaitDuration.Seconds())
	counter(d[6], float64(stats.MaxIdleClosed))
	counter(d[7], float64(stats.MaxLifetimeClosed))
}

// RegisterPoolMetrics registers pool statistics on reg. It is idempotent: a
// second App (tests build one per case) must not panic the whole test binary,
// so an existing pool collector is left in place.
func RegisterPoolMetrics(reg prometheus.Registerer, db *sql.DB) error {
	if db == nil {
		return nil
	}
	if err := reg.Register(newPoolCollector(db)); err != nil {
		var already prometheus.AlreadyRegisteredError
		if errors.As(err, &already) {
			return nil
		}
		return err
	}
	return nil
}
