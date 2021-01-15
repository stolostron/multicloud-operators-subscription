package config

import (
	"k8s.io/apimachinery/pkg/types"
)

// SubscriptionCMDOptions for command line flag parsing
type SubscriptionCMDoptions struct {
	MetricsAddr           string
	ClusterName           string
	ClusterNamespace      string
	HubConfigFilePathName string
	TLSKeyFilePathName    string
	TLSCrtFilePathName    string
	Syncid                *types.NamespacedName
	SyncInterval          int
	DisableTLS            bool
	Standalone            bool
	LeaseDurationSeconds  int
	CreateService         bool
	Beta                  bool
}
