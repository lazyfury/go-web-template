package buildinfo

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/example/go-web-template/internal/testutil"
)

func TestRegisterExposesAndUpdatesBuildInfo(t *testing.T) {
	reg := prometheus.NewRegistry()

	if err := Register(reg, "v1.2.3", "abc1234"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if got := testutil.GaugeValueFor(t, reg, "build_info", "version", "v1.2.3", "commit", "abc1234"); got != 1 {
		t.Fatalf("build_info{v1.2.3,abc1234} = %v, want 1", got)
	}

	// Re-registration (a second App in one process, or a binary upgraded in
	// place during tests) must not fail and must publish the new labels.
	if err := Register(reg, "v1.2.4", "def5678"); err != nil {
		t.Fatalf("second Register: %v", err)
	}
	if got := testutil.GaugeValueFor(t, reg, "build_info", "version", "v1.2.4", "commit", "def5678"); got != 1 {
		t.Fatalf("build_info{v1.2.4,def5678} = %v, want 1", got)
	}
}
