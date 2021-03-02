package kubernetes

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	Synchronizer = "appsub_synchronizer"
	InqueueKey   = "in_queue"
)

var (
	sycnInqueue = prometheus.NewGauge(prometheus.GaugeOpts{
		Subsystem: Synchronizer,
		Name:      InqueueKey,
		Help:      "Total number of orders in synchronizer queue",
	})
)

func init() {
	// Register custom metrics with the global prometheus registry
	metrics.Registry.MustRegister(sycnInqueue)
}
