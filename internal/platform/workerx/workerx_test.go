package workerx

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/example/go-web-template/internal/testutil"
)

func TestMarkSuccessRecordsTimestamp(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	MarkSuccess("test", at)

	got := testutil.GaugeValueFor(t, prometheus.DefaultGatherer,
		"worker_last_success_timestamp_seconds", "worker", "test")
	if got != float64(at.Unix()) {
		t.Fatalf("worker_last_success_timestamp_seconds = %v, want %d", got, at.Unix())
	}
}
