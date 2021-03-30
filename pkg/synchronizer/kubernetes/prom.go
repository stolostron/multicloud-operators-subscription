package kubernetes

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	Synchronizer = "synchronizer"
	Update_Error = "update_client"
)

var (
	updateTracker = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: Synchronizer,
		Name:      Update_Error,
		Help:      "Count the update failure",
	}, []string{"deployable", "subscription"})
)

func init() {
	metrics.Registry.MustRegister(updateTracker)
}
