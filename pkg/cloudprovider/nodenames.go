// Copyright The karpenter-provider-hetzner Authors.
// SPDX-License-Identifier: Apache-2.0

package cloudprovider

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	karpcp "sigs.k8s.io/karpenter/pkg/cloudprovider"

	apiv1 "sarak.sh/karpenter/provider-hetzner/pkg/apis/v1"
)

// defaultNodeNameTemplate is the CRD default of nodeNameTemplates, for a node
// class read without it.
const defaultNodeNameTemplate = "{{ .NodeClaim.Name }}"

// nodeNameData is what a node name template is given.
type nodeNameData struct {
	NodeClass nodeNameObject
	NodeClaim nodeNameObject
	Zone      string
	Rand      int64
}

// nodeNameObject is what a node name template sees of an object.
type nodeNameObject struct {
	Name   string
	Labels map[string]string
}

// nodeNameFuncs are the sprig functions but those that would put the
// controller's environment, which holds its credentials, or a DNS lookup into
// a server name.
var nodeNameFuncs = func() template.FuncMap {
	funcs := sprig.TxtFuncMap()
	for _, name := range []string{"env", "expandenv", "getHostByName"} {
		delete(funcs, name)
	}
	return funcs
}()

// renderNodeNames renders each template with data and a Rand of its own. A
// template that does not parse or run, or renders anything but a lowercase DNS
// label, is an error naming it.
func renderNodeNames(templates []string, data nodeNameData) ([]string, error) {
	names := make([]string, 0, len(templates))
	for _, text := range templates {
		tmpl, err := template.New("nodeName").Funcs(nodeNameFuncs).Parse(text)
		if err != nil {
			return nil, fmt.Errorf("parsing node name template %q: %w", text, err)
		}
		data.Rand = rand.Int64()
		var b strings.Builder
		if err := tmpl.Execute(&b, data); err != nil {
			return nil, fmt.Errorf("rendering node name template %q: %w", text, err)
		}
		name := b.String()
		if errs := validation.IsDNS1123Label(name); len(errs) > 0 {
			return nil, fmt.Errorf("node name template %q rendered %q: %s", text, name, strings.Join(errs, "; "))
		}
		names = append(names, name)
	}
	return names, nil
}

// nodeNameHolds keeps a name picked for one launch from being picked for
// another until the first launch's server exists or will not be created.
// Neither a Node nor a server shows the name before then.
type nodeNameHolds struct {
	mu   sync.Mutex
	held sets.Set[string]
}

// hold picks one of names at random among those neither in taken nor held,
// and holds it. ok is false when there is none.
func (h *nodeNameHolds) hold(names []string, taken sets.Set[string]) (name string, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	free := make([]string, 0, len(names))
	for _, n := range names {
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

// nodeName returns the name the server of nodeClaim in zone is created with,
// and release, to call once that server exists or will not be created. A
// template that fails is a NodeClassNotReadyError, and no free name an
// InsufficientCapacityError.
func (cp *CloudProvider) nodeName(ctx context.Context, nodeClass *apiv1.HCloudNodeClass, nodeClaim *karpv1.NodeClaim, zone string) (name string, release func(), err error) {
	templates := nodeClass.Spec.NodeNameTemplates
	if len(templates) == 0 {
		templates = []string{defaultNodeNameTemplate}
	}
	names, err := renderNodeNames(templates, nodeNameData{
		NodeClass: nodeNameObject{Name: nodeClass.Name, Labels: nodeClass.Labels},
		NodeClaim: nodeNameObject{Name: nodeClaim.Name, Labels: nodeClaim.Labels},
		Zone:      zone,
	})
	if err != nil {
		return "", nil, karpcp.NewNodeClassNotReadyError(fmt.Errorf("HCloudNodeClass %q: %w", nodeClass.Name, err))
	}
	taken, err := cp.takenNodeNames(ctx)
	if err != nil {
		return "", nil, err
	}
	name, ok := cp.nodeNameHolds.hold(names, taken)
	if !ok {
		return "", nil, karpcp.NewInsufficientCapacityError(fmt.Errorf(
			"HCloudNodeClass %q has no free node name for nodeclaim %s: %s are taken", nodeClass.Name, nodeClaim.Name, strings.Join(names, ", ")))
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
