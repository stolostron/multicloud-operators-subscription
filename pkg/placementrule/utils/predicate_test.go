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
	oldArgocdService2 = &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "argocd-server",
			Labels: map[string]string{},
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

	updateEvt := event.TypedUpdateEvent[*spokeClusterV1.ManagedCluster]{
		ObjectOld: oldCluster,
		ObjectNew: newCluster,
	}
	ret := instance.Update(updateEvt)
	g.Expect(ret).To(gomega.Equal(true))

	// Test AcmClusterSecretPredicateFunc
	instance2 := AcmClusterSecretPredicateFunc

	createEvt := event.TypedCreateEvent[*v1.Secret]{
		Object: newSecret,
	}
	ret = instance2.Create(createEvt)
	g.Expect(ret).To(gomega.Equal(true))

	updateEvt2 := event.TypedUpdateEvent[*v1.Secret]{
		ObjectOld: oldSecret,
		ObjectNew: newSecret,
	}

	ret = instance2.Update(updateEvt2)
	g.Expect(ret).To(gomega.Equal(true))

	delEvt := event.TypedDeleteEvent[*v1.Secret]{
		Object: newSecret,
	}
	ret = instance2.Delete(delEvt)
	g.Expect(ret).To(gomega.Equal(true))

	// Test ArgocdClusterSecretPredicateFunc
	instance3 := ArgocdClusterSecretPredicateFunc

	createEvt3 := event.TypedCreateEvent[*v1.Secret]{
		Object: newSecret,
	}
	ret = instance3.Create(createEvt3)
	g.Expect(ret).To(gomega.Equal(true))

	updateEvt3 := event.TypedUpdateEvent[*v1.Secret]{
		ObjectOld: oldSecret,
		ObjectNew: newSecret,
	}

	ret = instance3.Update(updateEvt3)
	g.Expect(ret).To(gomega.Equal(true))

	delEvt3 := event.TypedDeleteEvent[*v1.Secret]{
		Object: newSecret,
	}
	ret = instance3.Delete(delEvt3)
	g.Expect(ret).To(gomega.Equal(true))

	// Test ArgocdServerPredicateFunc
	instance4 := ArgocdServerPredicateFunc

	createEvt4 := event.TypedCreateEvent[*v1.Service]{
		Object: newArgocdService,
	}
	ret = instance4.Create(createEvt4)
	g.Expect(ret).To(gomega.Equal(true))

	updateEvt4 := event.TypedUpdateEvent[*v1.Service]{
		ObjectOld: oldArgocdService,
		ObjectNew: newArgocdService,
	}

	ret = instance4.Update(updateEvt4)
	g.Expect(ret).To(gomega.Equal(true))

	updateEvt4 = event.TypedUpdateEvent[*v1.Service]{
		ObjectOld: oldArgocdService2,
		ObjectNew: newArgocdService,
	}

	ret = instance4.Update(updateEvt4)
	g.Expect(ret).To(gomega.Equal(true))

	delEvt4 := event.TypedDeleteEvent[*v1.Service]{
		Object: newArgocdService,
	}
	ret = instance4.Delete(delEvt4)
	g.Expect(ret).To(gomega.Equal(true))

	// Test PlacementDecisionPredicateFunc
	instance5 := PlacementDecisionPredicateFunc

	createEvt5 := event.TypedCreateEvent[*clusterv1beta1.PlacementDecision]{
		Object: oldDecision,
	}
	ret = instance5.Create(createEvt5)
	g.Expect(ret).To(gomega.BeTrue())

	delEvt5 := event.TypedDeleteEvent[*clusterv1beta1.PlacementDecision]{
		Object: oldDecision,
	}
	ret = instance5.Delete(delEvt5)
	g.Expect(ret).To(gomega.BeTrue())

	updateEvt5 := event.TypedUpdateEvent[*clusterv1beta1.PlacementDecision]{
		ObjectOld: oldDecision,
		ObjectNew: newDecision,
	}
	ret = instance5.Update(updateEvt5)
	g.Expect(ret).To(gomega.BeFalse())

	// Test ManagedClusterSecretPredicateFunc
	instance6 := ManagedClusterSecretPredicateFunc

	updateEvt6 := event.TypedUpdateEvent[*v1.Secret]{
		ObjectNew: clusterSecret,
	}
	ret = instance6.Update(updateEvt6)
	g.Expect(ret).To(gomega.BeFalse())

	updateEvtNoSecret6 := event.TypedUpdateEvent[*v1.Secret]{
		ObjectNew: clusterNoSecret,
	}
	ret = instance6.Update(updateEvtNoSecret6)
	g.Expect(ret).To(gomega.BeTrue())

	createEvt6 := event.TypedCreateEvent[*v1.Secret]{
		Object: clusterSecret,
	}
	ret = instance6.Create(createEvt6)
	g.Expect(ret).To(gomega.BeFalse())

	createEvtNoSecret6 := event.TypedCreateEvent[*v1.Secret]{
		Object: clusterNoSecret,
	}
	ret = instance6.Create(createEvtNoSecret6)
	g.Expect(ret).To(gomega.BeTrue())

	delEvt6 := event.TypedDeleteEvent[*v1.Secret]{
		Object: clusterSecret,
	}
	ret = instance6.Delete(delEvt6)
	g.Expect(ret).To(gomega.BeTrue())

	delEvtNoSecret6 := event.TypedDeleteEvent[*v1.Secret]{
		Object: clusterNoSecret,
	}
	ret = instance6.Delete(delEvtNoSecret6)
	g.Expect(ret).To(gomega.BeFalse())
}
