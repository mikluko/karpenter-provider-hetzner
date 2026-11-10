// Copyright The karpenter-provider-hetzner Authors.
// SPDX-License-Identifier: Apache-2.0

package bootstrap

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	apiv1 "sarak.sh/mikluko/karpenter-provider-hetzner/pkg/apis/v1"
)

// TokenTTL is how long a node's join token stays valid. A NodeClaim whose node
// has not registered by then has already been given up on by Karpenter.
const TokenTTL = time.Hour

// Joiner mints each node's join token and renders the userData it boots with.
type Joiner struct {
	kubeClient   client.Client
	apiServerURL string
	caData       []byte
	now          func() time.Time
}

// NewJoiner returns a Joiner whose tokens join nodes to the API server at
// apiServerURL, trusting caData.
func NewJoiner(kubeClient client.Client, apiServerURL string, caData []byte) *Joiner {
	return &Joiner{kubeClient: kubeClient, apiServerURL: apiServerURL, caData: caData, now: time.Now}
}

// UserData creates a join token for nodeClaim and returns the userData for a
// server of nc. JOIN_TOKEN is a k0s worker join token whichever join runs.
// revoke deletes the token; call it when no server is created.
func (j *Joiner) UserData(ctx context.Context, nc *apiv1.HCloudNodeClass, nodeClaim *karpv1.NodeClaim) (userData string, revoke func(context.Context) error, err error) {
	parts, err := ResolveParts(ctx, j.kubeClient, &nc.Spec.Bootstrap)
	if err != nil {
		return "", nil, err
	}
	token, secret, err := NewTokenSecret(nodeClaim, j.now(), TokenTTL)
	if err != nil {
		return "", nil, err
	}
	joinToken, err := K0sJoinToken(j.apiServerURL, j.caData, token)
	if err != nil {
		return "", nil, err
	}
	userData, err = UserData(JoinEnv{
		JoinToken:      joinToken,
		NodeName:       nodeClaim.Name,
		RegisterTaints: RegisterTaints(nodeClaim),
	}, parts, nc.Spec.Bootstrap.K0sVersion)
	if err != nil {
		return "", nil, err
	}
	if err := j.kubeClient.Create(ctx, secret); err != nil {
		return "", nil, fmt.Errorf("creating join token secret: %w", err)
	}
	return userData, func(ctx context.Context) error {
		return client.IgnoreNotFound(j.kubeClient.Delete(ctx, secret))
	}, nil
}

// RegisterTaints returns the taints a node of nodeClaim registers with:
// karpenter.sh/unregistered, then the NodeClaim's taints and startup taints.
func RegisterTaints(nodeClaim *karpv1.NodeClaim) []string {
	taints := append([]corev1.Taint{karpv1.UnregisteredNoExecuteTaint}, nodeClaim.Spec.Taints...)
	taints = append(taints, nodeClaim.Spec.StartupTaints...)
	out := make([]string, len(taints))
	for i, t := range taints {
		out[i] = t.ToString()
	}
	return out
}
