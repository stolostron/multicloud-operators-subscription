package kubernetes

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	uzap "go.uber.org/zap"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type middleManStatus struct {
	clt    client.Client
	Logger logr.Logger
}

type middleMan struct {
	name   string
	c      cache.Cache
	clt    client.Client
	sclt   *middleManStatus
	Logger logr.Logger
}

func newMiddleMan(config *rest.Config, nsKey *types.NamespacedName) (*middleMan, error) {
	m := &middleMan{name: nsKey.Name}
	c, err := cache.New(config, cache.Options{Namespace: nsKey.Namespace})
	if err != nil {
		return nil, err
	}

	m.c = c

	clt, err := manager.DefaultNewClient(c, config, client.Options{})
	if err != nil {
		return nil, err
	}

	ms := &middleManStatus{clt: clt}
	m.clt = clt
	m.sclt = ms

	logger, err := uzap.NewDevelopment()
	if err != nil {
		return nil, err
	}

	l := zapr.NewLogger(logger)
	m.Logger = l.WithName(fmt.Sprintf("middleman-on-%s", mg.name))
	ms.Logger = l.WithName(fmt.Sprintf("middleman-status-on-%s", m.name))

	return m, nil
}

// next, we might want to have some middleware on the Get/List request with
// metric, then we will see how many request is coming from these

var _ client.Client = &middleMan{}

func (m *middleMan) Get(ctx context.Context, key types.NamespacedName, obj runtime.Object) error {
	m.Logger.Info(fmt.Sprintf("Get Request for kind %s with key %s", obj.GetObjectKind().GroupVersionKind().String(), key.String()))
	return m.clt.Get(ctx, key, obj)
}

func (m *middleMan) List(ctx context.Context, list runtime.Object, opts ...client.ListOption) error {
	opt := &client.ListOptions{}
	opt.ApplyOptions(opts)

	key, _ := client.ObjectKeyFromObject(list)
	m.Logger.Info(fmt.Sprintf("List Request for kind %s with key %s", list.GetObjectKind().GroupVersionKind().String(), key.String()))

	return m.clt.List(ctx, list, opt)
}

func (m *middleMan) Create(ctx context.Context, obj runtime.Object, opts ...client.CreateOption) error {
	opt := &client.CreateOptions{}
	opt.ApplyOptions(opts)

	key, _ := client.ObjectKeyFromObject(obj)
	m.Logger.Info(fmt.Sprintf("Create Request for kind %s with key %s", obj.GetObjectKind().GroupVersionKind().String(), key.String()))

	return m.clt.Create(ctx, obj, opt)
}

func (m *middleMan) Delete(ctx context.Context, obj runtime.Object, opts ...client.DeleteOption) error {
	opt := &client.DeleteOptions{}
	opt.ApplyOptions(opts)
	key, _ := client.ObjectKeyFromObject(obj)
	m.Logger.Info(fmt.Sprintf("Delete Request for kind %s with key %s", obj.GetObjectKind().GroupVersionKind().String(), key.String()))

	return m.clt.Delete(ctx, obj, opt)
}

func (m *middleMan) Update(ctx context.Context, obj runtime.Object, opts ...client.UpdateOption) error {
	opt := &client.UpdateOptions{}
	opt.ApplyOptions(opts)
	key, _ := client.ObjectKeyFromObject(obj)
	m.Logger.Info(fmt.Sprintf("Update Request for kind %s with key %s", obj.GetObjectKind().GroupVersionKind().String(), key.String()))

	return m.clt.Update(ctx, obj, opt)
}

func (m *middleMan) Patch(ctx context.Context, obj runtime.Object, patch client.Patch, opts ...client.PatchOption) error {
	opt := &client.PatchOptions{}
	opt.ApplyOptions(opts)
	key, _ := client.ObjectKeyFromObject(obj)
	m.Logger.Info(fmt.Sprintf("Patch Request for kind %s with key %s", obj.GetObjectKind().GroupVersionKind().String(), key.String()))
	return m.clt.Patch(ctx, obj, patch, opt)
}

func (m *middleMan) DeleteAllOf(ctx context.Context, obj runtime.Object, opts ...client.DeleteAllOfOption) error {
	opt := &client.DeleteAllOfOptions{}
	opt.ApplyOptions(opts)
	key, _ := client.ObjectKeyFromObject(obj)
	m.Logger.Info(fmt.Sprintf("DeleteAllOf Request for kind %s with key %s", obj.GetObjectKind().GroupVersionKind().String(), key.String()))
	return m.clt.DeleteAllOf(ctx, obj, opt)
}

func (m *middleMan) Status() client.StatusWriter {
	return m.sclt
}

func (ms *middleManStatus) Update(ctx context.Context, obj runtime.Object, opts ...client.UpdateOption) error {
	opt := &client.UpdateOptions{}
	opt.ApplyOptions(opts)
	key, _ := client.ObjectKeyFromObject(obj)
	ms.Logger.Info(fmt.Sprintf("Update Request for kind %s with key %s", obj.GetObjectKind().GroupVersionKind().String(), key.String()))
	return ms.clt.Status().Update(ctx, obj, opt)
}

func (ms *middleManStatus) Patch(ctx context.Context, obj runtime.Object, patch client.Patch, opts ...client.PatchOption) error {
	opt := &client.PatchOptions{}
	opt.ApplyOptions(opts)
	key, _ := client.ObjectKeyFromObject(obj)
	ms.Logger.Info(fmt.Sprintf("Patch Request for kind %s with key %s", obj.GetObjectKind().GroupVersionKind().String(), key.String()))
	return ms.clt.Status().Patch(ctx, obj, patch, opt)
}
