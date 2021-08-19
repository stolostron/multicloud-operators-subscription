package kubernetes

import (
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type cachedClient struct {
	clt         client.Client
	clientCache cache.Cache
}

func newCachedClient(config *rest.Config, nsKey *types.NamespacedName) (*cachedClient, error) {
	m := &cachedClient{clt: nil, clientCache: nil}
	cache, err := cache.New(config, cache.Options{Namespace: nsKey.Namespace})

	if err != nil {
		return nil, fmt.Errorf("failed to create cached client, err: %w", err)
	}

	m.clientCache = cache

	clt, err := client.New(config, client.Options{})
	if err != nil {
		return nil, fmt.Errorf("failed to create client, err: %w", err)
	}

	newDelegatingClientInput := client.NewDelegatingClientInput{
		CacheReader:       cache,
		Client:            clt,
		UncachedObjects:   []client.Object{},
		CacheUnstructured: true,
	}

	m.clt, err = client.NewDelegatingClient(newDelegatingClientInput)
	if err != nil {
		return nil, fmt.Errorf("failed to create cached client, err: %w", err)
	}

	return m, nil
}
