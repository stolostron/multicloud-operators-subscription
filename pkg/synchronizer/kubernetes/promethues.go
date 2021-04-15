package kubernetes

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	Synchronizer = "synchronizer"
	UpdateError  = "cached_client_update_total"
)

var (
	updateTracker = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: Synchronizer,
		Name:      UpdateError,
		Help:      "Count the update failure",
	}, []string{"caller", "status"})
)

func init() {
	metrics.Registry.MustRegister(updateTracker)
}
