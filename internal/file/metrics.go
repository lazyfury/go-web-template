package file

import (
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Upload/storage metrics. Labels are closed sets: the MIME group is derived
// from the type prefix, never the filename, owner or storage key, so no PII can
// end up in Prometheus (see context.md §7).
var (
	uploadsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "file_uploads_total",
			Help: "Upload attempts by result (ok, error).",
		},
		[]string{"result"},
	)
	uploadBytesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "file_upload_bytes_total",
			Help: "Bytes accepted by uploads, grouped by MIME family.",
		},
		[]string{"mime_group"},
	)
	storageOpsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "file_storage_operations_total",
			Help: "Object storage operations by driver, operation and result.",
		},
		[]string{"driver", "operation", "result"},
	)
	storageOpDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "file_storage_operation_duration_seconds",
			Help:    "Latency of one object storage operation.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"driver", "operation"},
	)
)

func init() {
	prometheus.MustRegister(uploadsTotal, uploadBytesTotal, storageOpsTotal, storageOpDuration)
}

// observeUpload records one upload attempt.
func observeUpload(mimeType string, size int64, result string) {
	uploadsTotal.WithLabelValues(result).Inc()
	if result == "ok" {
		uploadBytesTotal.WithLabelValues(mimeGroup(mimeType)).Add(float64(size))
	}
}

// observeStorageOp records one storage call.
func observeStorageOp(driver, operation, result string, elapsed time.Duration) {
	storageOpsTotal.WithLabelValues(driver, operation, result).Inc()
	storageOpDuration.WithLabelValues(driver, operation).Observe(elapsed.Seconds())
}

// mimeGroup collapses a content type into a small closed set.
func mimeGroup(mimeType string) string {
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return "image"
	case strings.HasPrefix(mimeType, "text/"):
		return "text"
	case strings.HasPrefix(mimeType, "video/"), strings.HasPrefix(mimeType, "audio/"):
		return "media"
	case mimeType == "application/pdf":
		return "pdf"
	case mimeType == "application/zip":
		return "archive"
	case mimeType == "":
		return "unknown"
	default:
		return "other"
	}
}
