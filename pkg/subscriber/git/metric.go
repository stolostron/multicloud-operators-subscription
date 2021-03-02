package git

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	GitSubscriber             = "appsub_git_subscriber"
	AwakeKey                  = "awake_total"
	SubscriberDuration        = "duration_seconds"
	StartedSubscriber         = "total"
	ActiveSubscriberGoroutine = "active_total"
)

var (
	gitSubscriberAwake = prometheus.NewGauge(prometheus.GaugeOpts{
		Subsystem: GitSubscriber,
		Name:      AwakeKey,
		Help:      "Total number of awake git subscribers",
	})

	gitSubscriberTime = prometheus.NewHistogram(prometheus.HistogramOpts{
		Subsystem: GitSubscriber,
		Name:      SubscriberDuration,
		Help:      "How long in seconds an subscriber cycle is",
	})

	gitSubscriberGoroutinue = prometheus.NewGauge(prometheus.GaugeOpts{
		Subsystem: GitSubscriber,
		Name:      ActiveSubscriberGoroutine,
		Help:      "How many gorountine is running",
	})
)

func init() {
	// Register custom metrics with the global prometheus registry
	metrics.Registry.MustRegister(gitSubscriberAwake, gitSubscriberTime,
		gitSubscriberGoroutinue)
}
