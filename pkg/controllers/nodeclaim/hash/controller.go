// Copyright The karpenter-provider-hetzner Authors.
// SPDX-License-Identifier: Apache-2.0

// Package hash stamps NodeClaims with the hash of their HCloudNodeClass when
// they carry none, or one computed under another hash version.
package hash

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	apiv1 "sarak.sh/mikluko/karpenter-provider-hetzner/pkg/apis/v1"
)

// Controller restamps NodeClaims whose node class hash version is not
// apiv1.HCloudNodeClassHashVersion with their node class's current hash. Such
// a NodeClaim is taken to match its node class as it stands, so only edits
// made afterwards drift it.
type Controller struct {
	kubeClient client.Client
}

func NewController(kubeClient client.Client) *Controller {
	return &Controller{kubeClient: kubeClient}
}

func (c *Controller) Name() string { return "nodeclaim.nodeclasshash" }

func (c *Controller) Reconcile(ctx context.Context, nodeClaim *karpv1.NodeClaim) (reconcile.Result, error) {
	ref := nodeClaim.Spec.NodeClassRef
	if ref == nil || ref.Group != apiv1.Group || ref.Kind != "HCloudNodeClass" {
		return reconcile.Result{}, nil
	}
	if nodeClaim.Annotations[apiv1.AnnotationHCloudNodeClassHashVersion] == apiv1.HCloudNodeClassHashVersion {
		return reconcile.Result{}, nil
	}
	nodeClass := &apiv1.HCloudNodeClass{}
	if err := c.kubeClient.Get(ctx, types.NamespacedName{Name: ref.Name}, nodeClass); err != nil {
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, fmt.Errorf("getting HCloudNodeClass %q: %w", ref.Name, err)
	}
	stored := nodeClaim.DeepCopy()
	if nodeClaim.Annotations == nil {
		nodeClaim.Annotations = map[string]string{}
	}
	nodeClaim.Annotations[apiv1.AnnotationHCloudNodeClassHash] = nodeClass.Hash()
	nodeClaim.Annotations[apiv1.AnnotationHCloudNodeClassHashVersion] = apiv1.HCloudNodeClassHashVersion
	if err := c.kubeClient.Patch(ctx, nodeClaim, client.MergeFrom(stored)); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}
	return reconcile.Result{}, nil
}

func (c *Controller) Register(_ context.Context, m manager.Manager) error {
	return controllerruntime.NewControllerManagedBy(m).
		For(&karpv1.NodeClaim{}).
		Named(c.Name()).
		Complete(reconcile.AsReconciler(m.GetClient(), c))
}
