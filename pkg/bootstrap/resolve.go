// Copyright The karpenter-provider-hetzner Authors.
// SPDX-License-Identifier: Apache-2.0

package bootstrap

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "sarak.sh/mikluko/karpenter-provider-hetzner/pkg/apis/v1"
)

// ErrKeyMissing reports a Secret that exists without the referenced key, or
// with it empty.
var ErrKeyMissing = errors.New("secret has no non-empty key")

// ResolvePart returns the content of p, reading its Secret when it has one. A
// nil p resolves to "". A missing Secret returns the API server's NotFound.
func ResolvePart(ctx context.Context, r client.Reader, p *apiv1.UserDataPart) (string, error) {
	if p == nil {
		return "", nil
	}
	ref := p.SecretRef
	if ref == nil {
		return p.Inline, nil
	}
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}, secret); err != nil {
		return "", fmt.Errorf("reading secret %s/%s: %w", ref.Namespace, ref.Name, err)
	}
	if v := secret.Data[ref.Key]; len(v) > 0 {
		return string(v), nil
	}
	return "", fmt.Errorf("%w %q in %s/%s", ErrKeyMissing, ref.Key, ref.Namespace, ref.Name)
}

// ResolveParts resolves every part of b.
func ResolveParts(ctx context.Context, r client.Reader, b *apiv1.Bootstrap) (Parts, error) {
	var parts Parts
	for _, p := range []struct {
		dst  *string
		src  *apiv1.UserDataPart
		name string
	}{
		{&parts.BeforeJoin, b.BeforeJoin, "beforeJoin"},
		{&parts.Join, b.Join, "join"},
		{&parts.AfterJoin, b.AfterJoin, "afterJoin"},
	} {
		v, err := ResolvePart(ctx, r, p.src)
		if err != nil {
			return Parts{}, fmt.Errorf("%s: %w", p.name, err)
		}
		*p.dst = v
	}
	return parts, nil
}
