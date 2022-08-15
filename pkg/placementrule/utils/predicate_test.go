package utils

import (
	"testing"

	"github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	spokeClusterV1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
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

	oldDecision = &clusterv1beta1.PlacementDecision{
		TypeMeta: metav1.TypeMeta{
			Kind:       "PlacementDecision",
			APIVersion: "cluster.open-cluster-management.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster1",
		},
		Status: clusterv1beta1.PlacementDecisionStatus{
			Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1", Reason: "running"}},
		},
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

	newDecision = &clusterv1beta1.PlacementDecision{
		TypeMeta: metav1.TypeMeta{
			Kind:       "PlacementDecision",
			APIVersion: "cluster.open-cluster-management.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster1",
		},
		Status: clusterv1beta1.PlacementDecisionStatus{
			Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1", Reason: "running"}},
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

	clusterSecret = &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster1-cluster-secret",
			Labels: map[string]string{
				ArgocdClusterSecretLabel: "fake-secret",
			},
		},
	}

	clusterNoSecret = &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "cluster1-cluster-secret",
			Labels: map[string]string{},
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

	// Test PlacementDecisionPredicateFunc
	instance = PlacementDecisionPredicateFunc

	createEvt = event.CreateEvent{
		Object: oldDecision,
	}
	ret = instance.Create(createEvt)
	g.Expect(ret).To(gomega.BeTrue())

	delEvt = event.DeleteEvent{
		Object: oldDecision,
	}
	ret = instance.Delete(delEvt)
	g.Expect(ret).To(gomega.BeTrue())

	updateEvt = event.UpdateEvent{
		ObjectOld: oldDecision,
		ObjectNew: newDecision,
	}
	ret = instance.Update(updateEvt)
	g.Expect(ret).To(gomega.BeFalse())

	// Test ManagedClusterSecretPredicateFunc
	instance = ManagedClusterSecretPredicateFunc

	updateEvt = event.UpdateEvent{
		ObjectNew: clusterSecret,
	}
	ret = instance.Update(updateEvt)
	g.Expect(ret).To(gomega.BeFalse())

	updateEvtNoSecret := event.UpdateEvent{
		ObjectNew: clusterNoSecret,
	}
	ret = instance.Update(updateEvtNoSecret)
	g.Expect(ret).To(gomega.BeTrue())

	createEvt = event.CreateEvent{
		Object: clusterSecret,
	}
	ret = instance.Create(createEvt)
	g.Expect(ret).To(gomega.BeFalse())

	createEvtNoSecret := event.CreateEvent{
		Object: clusterNoSecret,
	}
	ret = instance.Create(createEvtNoSecret)
	g.Expect(ret).To(gomega.BeTrue())

	delEvt = event.DeleteEvent{
		Object: clusterSecret,
	}
	ret = instance.Delete(delEvt)
	g.Expect(ret).To(gomega.BeTrue())

	delEvtNoSecret := event.DeleteEvent{
		Object: clusterNoSecret,
	}
	ret = instance.Delete(delEvtNoSecret)
	g.Expect(ret).To(gomega.BeFalse())

}
