package utils

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func CreateAppsubConfigMap(clt client.Client, appsubKey types.NamespacedName) error {
	cm := &corev1.ConfigMap{}

	ctx := context.TODO()

	err := clt.Get(ctx, appsubKey, cm)
	if err == nil {
		return nil
	}

	if k8serr.IsNotFound(err) {
		cm.SetName(appsubKey.Name)
		cm.SetNamespace(appsubKey.Namespace)

		return clt.Create(ctx, cm)
	}

	return err
}

// DeleteAppsubConfigMap
func DeleteAppsubConfigMap(clt client.Client, appsubKey types.NamespacedName) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      appsubKey.Name,
			Namespace: appsubKey.Namespace,
		},
	}

	return clt.Delete(context.TODO(), cm)
}
