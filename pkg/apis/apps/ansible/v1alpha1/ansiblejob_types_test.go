// Copyright 2021 The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1alpha1

import (
	"context"
	"testing"

	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var (
	key = types.NamespacedName{
		Name:      "foo",
		Namespace: "default",
	}

	workflowKey = types.NamespacedName{
		Name:      "foo-workflow",
		Namespace: "default",
	}

	deploy = &AnsibleJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
		},
		Spec: AnsibleJobSpec{
			TowerAuthSecretName: "tower-secret",
		},
	}

	workflowDeploy = &AnsibleJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workflowKey.Name,
			Namespace: workflowKey.Namespace,
		},
		Spec: AnsibleJobSpec{
			TowerAuthSecretName:  "tower-secret",
			WorkflowTemplateName: "workflow-demo",
			JobTags:              "job,tags",
			SkipTags:             "skip,tags",
		},
	}
)

func TestAnsible(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Test Create
	fetched := &AnsibleJob{}

	created := deploy.DeepCopy()
	g.Expect(c.Create(context.TODO(), created)).NotTo(gomega.HaveOccurred())
	g.Expect(c.Get(context.TODO(), key, fetched)).NotTo(gomega.HaveOccurred())

	// Marshal event time
	e, err := EventTime{metav1.Time{}}.MarshalJSON()
	g.Expect(e).To(gomega.Equal([]byte(`"0001-01-01T00:00:00"`)))
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Unmarshal event time
	newE := EventTime{metav1.Time{}}
	newE.UnmarshalJSON([]byte(""))
	g.Expect(newE).To(gomega.Equal(EventTime{metav1.Time{}}))
}

func TestAnsibleWorkflow(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	g.Expect(c.Create(context.TODO(), workflowDeploy)).NotTo(gomega.HaveOccurred())

	fetched := &AnsibleJob{}
	g.Expect(c.Get(context.TODO(), workflowKey, fetched)).NotTo(gomega.HaveOccurred())

	g.Expect(fetched.Spec.WorkflowTemplateName).To(gomega.Equal("workflow-demo"))
	g.Expect(fetched.Spec.JobTags).To(gomega.Equal("job,tags"))
	g.Expect(fetched.Spec.SkipTags).To(gomega.Equal("skip,tags"))
}
