package testutil

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

// CounterValue sums a counter metric across all label values. Tests must assert
// deltas, never absolute values: the default registry is shared by the whole
// test binary, so other tests (and parallel subtests) move the same counters.
func CounterValue(tb testing.TB, name string) float64 {
	tb.Helper()
	return CounterValueFor(tb, name)
}

// CounterValueFor sums only the series whose labels match every pair given as
// key, value, key, value...
func CounterValueFor(tb testing.TB, name string, labels ...string) float64 {
	tb.Helper()
	if len(labels)%2 != 0 {
		tb.Fatalf("CounterValueFor(%s): labels must be key/value pairs, got %d values", name, len(labels))
	}
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		tb.Fatalf("gather metrics: %v", err)
	}
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		total := 0.0
		for _, metric := range family.GetMetric() {
			matched := true
			for i := 0; i < len(labels); i += 2 {
				found := false
				for _, pair := range metric.GetLabel() {
					if pair.GetName() == labels[i] && pair.GetValue() == labels[i+1] {
						found = true
						break
					}
				}
				if !found {
					matched = false
					break
				}
			}
			if !matched {
				continue
			}
			switch {
			case metric.GetCounter() != nil:
				total += metric.GetCounter().GetValue()
			case metric.GetGauge() != nil:
				total += metric.GetGauge().GetValue()
			}
		}
		return total
	}
	return 0
}
