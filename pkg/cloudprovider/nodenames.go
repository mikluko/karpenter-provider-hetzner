// Copyright The karpenter-provider-hetzner Authors.
// SPDX-License-Identifier: Apache-2.0

package cloudprovider

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	karpcp "sigs.k8s.io/karpenter/pkg/cloudprovider"

	apiv1 "sarak.sh/mikluko/karpenter-provider-hetzner/pkg/apis/v1"
)

// nodeNameHolds keeps a name picked for one launch from being picked for
// another until the first launch's server exists or will not be created.
// Neither a Node nor a server shows the name before then.
type nodeNameHolds struct {
	mu   sync.Mutex
	held sets.Set[string]
}

// hold picks a name of pool at random among those neither in taken nor held,
// and holds it. ok is false when there is none.
func (h *nodeNameHolds) hold(pool []string, taken sets.Set[string]) (name string, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	free := make([]string, 0, len(pool))
	for _, n := range pool {
		if !taken.Has(n) && !h.held.Has(n) {
			free = append(free, n)
		}
	}
	if len(free) == 0 {
		return "", false
	}
	name = free[rand.IntN(len(free))]
	if h.held == nil {
		h.held = sets.New[string]()
	}
	h.held.Insert(name)
	return name, true
}

func (h *nodeNameHolds) release(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.held.Delete(name)
}

// nodeName returns the name the server of nodeClaim is created with, and
// release, to call once that server exists or will not be created. A pool with
// no free name is an InsufficientCapacityError.
func (cp *CloudProvider) nodeName(ctx context.Context, nodeClass *apiv1.HCloudNodeClass, nodeClaim *karpv1.NodeClaim) (name string, release func(), err error) {
	if len(nodeClass.Spec.NodeNames) == 0 {
		return nodeClaim.Name, func() {}, nil
	}
	taken, err := cp.takenNodeNames(ctx)
	if err != nil {
		return "", nil, err
	}
	name, ok := cp.nodeNameHolds.hold(nodeClass.Spec.NodeNames, taken)
	if !ok {
		return "", nil, karpcp.NewInsufficientCapacityError(fmt.Errorf(
			"HCloudNodeClass %q has no free name in nodeNames for nodeclaim %s", nodeClass.Name, nodeClaim.Name))
	}
	return name, func() { cp.nodeNameHolds.release(name) }, nil
}

// takenNodeNames returns the names of every Node of the cluster and every
// server of the Hetzner project.
func (cp *CloudProvider) takenNodeNames(ctx context.Context) (sets.Set[string], error) {
	names, err := cp.instanceProvider.Names(ctx)
	if err != nil {
		return nil, err
	}
	taken := sets.New(names...)
	nodes := &corev1.NodeList{}
	if err := cp.kubeClient.List(ctx, nodes); err != nil {
		return nil, fmt.Errorf("listing nodes: %w", err)
	}
	for _, n := range nodes.Items {
		taken.Insert(n.Name)
	}
	return taken, nil
}
