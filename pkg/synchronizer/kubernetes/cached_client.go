package kubernetes

import (
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type cachedClient struct {
	clt    client.Client
	cCache cache.Cache
}

func newCachedClient(config *rest.Config, nsKey *types.NamespacedName) (*cachedClient, error) {
	m := &cachedClient{clt: nil, cCache: nil}
	c, err := cache.New(config, cache.Options{Namespace: nsKey.Namespace})

	if err != nil {
		return nil, fmt.Errorf("failed to create cached client, err: %w", err)
	}

	m.cCache = c

	clt, err := manager.DefaultNewClient(c, config, client.Options{})
	if err != nil {
		return nil, fmt.Errorf("failed to create cached client, err: %w", err)
	}

	m.clt = clt

	return m, nil
}
