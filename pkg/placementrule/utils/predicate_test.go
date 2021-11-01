package utils

import (
	"testing"

	"github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	spokeClusterV1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

var (
	oldClusterCond1 = metav1.Condition{
		Type:   "ManagedClusterConditionAvailable",
		Status: "true",
	}

	oldClusterCond2 = metav1.Condition{
		Type:   "HubAcceptedManagedCluster",
		Status: "true",
	}

	oldCluster = &spokeClusterV1.ManagedCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ManagedCluster",
			APIVersion: "cluster.open-cluster-management.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster1",
			Labels: map[string]string{
				"name": "cluster1",
				"key1": "c1v1",
				"key2": "c1v2",
			},
		},
		Status: spokeClusterV1.ManagedClusterStatus{
			Conditions: []metav1.Condition{oldClusterCond1, oldClusterCond2},
		},
	}

	newClusterCond = metav1.Condition{
		Type:   "ManagedClusterConditionAvailable",
		Status: "true",
	}

	newCluster = &spokeClusterV1.ManagedCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ManagedCluster",
			APIVersion: "cluster.open-cluster-management.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster1",
			Labels: map[string]string{
				"name": "cluster1",
				"key1": "c1v1",
				"key2": "c1v2",
			},
		},
		Status: spokeClusterV1.ManagedClusterStatus{
			Conditions: []metav1.Condition{newClusterCond},
		},
	}

	oldSecret = &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster1-cluster-secret",
			Labels: map[string]string{
				"apps.open-cluster-management.io/secret-type": "non-acm-cluster",
			},
		},
	}

	newSecret = &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster1-cluster-secret",
			Labels: map[string]string{
				"apps.open-cluster-management.io/secret-type": "acm-cluster",
				"apps.open-cluster-management.io/acm-cluster": "true",
			},
		},
	}

	oldArgocdService = &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "argocd-server",
			Labels: map[string]string{
				"app.kubernetes.io/part-of":   "argocd",
				"app.kubernetes.io/component": "server",
			},
		},
	}

	newArgocdService = &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "argocd-server",
			Labels: map[string]string{
				"app.kubernetes.io/part-of":   "argocd",
				"app.kubernetes.io/component": "server",
			},
		},
	}
)

func TestPredicate(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Test ClusterPredicateFunc
	instance := ClusterPredicateFunc

	updateEvt := event.UpdateEvent{
		ObjectOld: oldCluster,
		ObjectNew: newCluster,
	}
	ret := instance.Update(updateEvt)
	g.Expect(ret).To(gomega.Equal(true))

	// Test AcmClusterSecretPredicateFunc
	instance = AcmClusterSecretPredicateFunc

	createEvt := event.CreateEvent{
		Object: newSecret,
	}
	ret = instance.Create(createEvt)
	g.Expect(ret).To(gomega.Equal(true))

	updateEvt = event.UpdateEvent{
		ObjectOld: oldSecret,
		ObjectNew: newSecret,
	}

	ret = instance.Update(updateEvt)
	g.Expect(ret).To(gomega.Equal(true))

	delEvt := event.DeleteEvent{
		Object: newSecret,
	}
	ret = instance.Delete(delEvt)
	g.Expect(ret).To(gomega.Equal(true))

	// Test ArgocdClusterSecretPredicateFunc
	instance = ArgocdClusterSecretPredicateFunc

	createEvt = event.CreateEvent{
		Object: newSecret,
	}
	ret = instance.Create(createEvt)
	g.Expect(ret).To(gomega.Equal(true))

	updateEvt = event.UpdateEvent{
		ObjectOld: oldSecret,
		ObjectNew: newSecret,
	}

	ret = instance.Update(updateEvt)
	g.Expect(ret).To(gomega.Equal(true))

	delEvt = event.DeleteEvent{
		Object: newSecret,
	}
	ret = instance.Delete(delEvt)
	g.Expect(ret).To(gomega.Equal(true))

	// Test ArgocdServerPredicateFunc
	instance = ArgocdServerPredicateFunc

	createEvt = event.CreateEvent{
		Object: newArgocdService,
	}
	ret = instance.Create(createEvt)
	g.Expect(ret).To(gomega.Equal(true))

	updateEvt = event.UpdateEvent{
		ObjectOld: oldArgocdService,
		ObjectNew: newArgocdService,
	}

	ret = instance.Update(updateEvt)
	g.Expect(ret).To(gomega.Equal(true))

	delEvt = event.DeleteEvent{
		Object: newArgocdService,
	}
	ret = instance.Delete(delEvt)
	g.Expect(ret).To(gomega.Equal(true))
}
