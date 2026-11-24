// Copyright The karpenter-provider-hetzner Authors.
// SPDX-License-Identifier: Apache-2.0

package bootstrap

import (
	"strings"
	"testing"

	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "sarak.sh/mikluko/karpenter-provider-hetzner/pkg/apis/v1"
)

func joinOf(t *testing.T, nc *apiv1.HCloudNodeClass) string {
	t.Helper()
	j := NewJoiner(fake.NewClientBuilder().WithScheme(scheme.Scheme).Build(), "https://api.example:6443", []byte("CA"))
	ud, _, err := j.UserData(t.Context(), nc, nodeClaim())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range parseUserData(t, ud) {
		if p.filename == filenameJoin {
			return p.content
		}
	}
	t.Fatal("userData has no join part")
	return ""
}

func TestJoiner_GeneratedJoinFollowsFamily(t *testing.T) {
	debian := &apiv1.HCloudNodeClass{Spec: apiv1.HCloudNodeClassSpec{
		ImageSelector: apiv1.ImageSelector{Family: apiv1.FamilyDebian},
		Bootstrap:     apiv1.Bootstrap{K0sVersion: "v1.36.6+k0s.0"},
	}}
	if join := joinOf(t, debian); !strings.Contains(join, "K0S_VERSION=v1.36.6+k0s.0") {
		t.Errorf("debian join does not install k0s:\n%s", join)
	}

	nixos := &apiv1.HCloudNodeClass{Spec: apiv1.HCloudNodeClassSpec{
		ImageSelector: apiv1.ImageSelector{Family: apiv1.FamilyNixOS, Selector: map[string]string{"role": "k0s-worker"}},
	}}
	if join := joinOf(t, nixos); join != NixOSJoin {
		t.Errorf("nixos join = %q, want NixOSJoin", join)
	}

	replaced := nixos.DeepCopy()
	replaced.Spec.Bootstrap.Join = &apiv1.UserDataPart{Inline: "#!/bin/sh\necho mine\n"}
	if join := joinOf(t, replaced); join != "#!/bin/sh\necho mine\n" {
		t.Errorf("a replaced nixos join = %q, want the replacement", join)
	}
}

// TestNixOSJoin pins what the join hands the image's k0s.service: the token at
// its tokenFile, and REGISTER_TAINTS and K0S_KUBELET_ARGS in its
// EnvironmentFile, which image/worker.nix names.
func TestNixOSJoin(t *testing.T) {
	for _, want := range []string{
		". " + JoinEnvPath + "\n",
		`printf '%s' "$JOIN_TOKEN" > /etc/k0s/join-token`,
		"cat > /etc/karpenter-hetzner/k0s.env <<EOF\n",
		"REGISTER_TAINTS=$REGISTER_TAINTS\n",
		`K0S_KUBELET_ARGS="--provider-id=hcloud://$server_id --hostname-override=$NODE_NAME ${KUBELET_EXTRA_ARGS:-}"` + "\n",
		"systemctl start k0s\n",
	} {
		if !strings.Contains(NixOSJoin, want) {
			t.Errorf("NixOSJoin lacks %q:\n%s", want, NixOSJoin)
		}
	}
}
