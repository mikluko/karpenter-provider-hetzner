// Copyright The karpenter-provider-hetzner Authors.
// SPDX-License-Identifier: Apache-2.0

package hash

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	apiv1 "sarak.sh/mikluko/karpenter-provider-hetzner/pkg/apis/v1"
)

func newNodeClass() *apiv1.HCloudNodeClass {
	return &apiv1.HCloudNodeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
		Spec: apiv1.HCloudNodeClassSpec{
			Locations:     []string{"nbg1"},
			ImageSelector: apiv1.ImageSelector{Family: "debian"},
			Bootstrap:     apiv1.Bootstrap{K0sVersion: "v1.34.1+k0s.0"},
		},
	}
}

func newNodeClaim(annotations map[string]string) *karpv1.NodeClaim {
	nc := &karpv1.NodeClaim{ObjectMeta: metav1.ObjectMeta{Name: "claim", Annotations: annotations}}
	nc.Spec.NodeClassRef = &karpv1.NodeClassReference{Name: "default", Group: apiv1.Group, Kind: "HCloudNodeClass"}
	return nc
}

func reconcileAndGet(t *testing.T, nodeClaim *karpv1.NodeClaim, objs ...client.Object) *karpv1.NodeClaim {
	t.Helper()
	_ = apiv1.SchemeBuilder.AddToScheme(scheme.Scheme)
	kube := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(append(objs, nodeClaim)...).Build()
	if _, err := NewController(kube).Reconcile(context.Background(), nodeClaim.DeepCopy()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	got := &karpv1.NodeClaim{}
	if err := kube.Get(context.Background(), client.ObjectKeyFromObject(nodeClaim), got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestReconcile_StampsUnstampedNodeClaim(t *testing.T) {
	nodeClass := newNodeClass()
	got := reconcileAndGet(t, newNodeClaim(map[string]string{"keep": "me"}), nodeClass)
	if h := got.Annotations[apiv1.AnnotationHCloudNodeClassHash]; h != nodeClass.Hash() {
		t.Errorf("hash = %q, want %q", h, nodeClass.Hash())
	}
	if v := got.Annotations[apiv1.AnnotationHCloudNodeClassHashVersion]; v != apiv1.HCloudNodeClassHashVersion {
		t.Errorf("hash version = %q, want %q", v, apiv1.HCloudNodeClassHashVersion)
	}
	if got.Annotations["keep"] != "me" {
		t.Error("existing annotation dropped")
	}
}

func TestReconcile_RestampsOtherHashVersion(t *testing.T) {
	nodeClass := newNodeClass()
	got := reconcileAndGet(t, newNodeClaim(map[string]string{
		apiv1.AnnotationHCloudNodeClassHash:        "stale",
		apiv1.AnnotationHCloudNodeClassHashVersion: "v0",
	}), nodeClass)
	if h := got.Annotations[apiv1.AnnotationHCloudNodeClassHash]; h != nodeClass.Hash() {
		t.Errorf("hash = %q, want %q", h, nodeClass.Hash())
	}
}

func TestReconcile_KeepsCurrentVersionHash(t *testing.T) {
	got := reconcileAndGet(t, newNodeClaim(map[string]string{
		apiv1.AnnotationHCloudNodeClassHash:        "launched",
		apiv1.AnnotationHCloudNodeClassHashVersion: apiv1.HCloudNodeClassHashVersion,
	}), newNodeClass())
	if h := got.Annotations[apiv1.AnnotationHCloudNodeClassHash]; h != "launched" {
		t.Errorf("hash = %q, want the launch-time hash kept", h)
	}
}

func TestReconcile_IgnoresForeignNodeClass(t *testing.T) {
	nodeClaim := newNodeClaim(nil)
	nodeClaim.Spec.NodeClassRef = &karpv1.NodeClassReference{Name: "default", Group: "karpenter.k8s.aws", Kind: "EC2NodeClass"}
	got := reconcileAndGet(t, nodeClaim, newNodeClass())
	if _, ok := got.Annotations[apiv1.AnnotationHCloudNodeClassHash]; ok {
		t.Error("stamped a NodeClaim of another provider's node class")
	}
}

func TestReconcile_MissingNodeClass(t *testing.T) {
	got := reconcileAndGet(t, newNodeClaim(nil))
	if _, ok := got.Annotations[apiv1.AnnotationHCloudNodeClassHash]; ok {
		t.Error("stamped a NodeClaim whose node class does not exist")
	}
}
