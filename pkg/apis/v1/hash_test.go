// Copyright The karpenter-provider-hetzner Authors.
// SPDX-License-Identifier: Apache-2.0

package v1

import "testing"

func hashedNodeClass() *HCloudNodeClass {
	on := true
	return &HCloudNodeClass{Spec: HCloudNodeClassSpec{
		Locations:        []string{"nbg1"},
		ImageSelector:    ImageSelector{Family: "debian", Version: "13"},
		NetworkID:        1,
		FirewallIDs:      []int64{7},
		SSHKeyIDs:        []int64{3, 4},
		Labels:           map[string]string{"team": "a"},
		Bootstrap:        Bootstrap{K0sVersion: "v1.34.1+k0s.0", BeforeJoin: &UserDataPart{Inline: "#!/bin/sh\n"}},
		EnablePublicIPv4: &on,
		EnablePublicIPv6: &on,
	}}
}

func TestHash_ChangesWithFieldsNotReadableFromServer(t *testing.T) {
	off := false
	for name, edit := range map[string]func(*HCloudNodeClassSpec){
		"beforeJoin": func(s *HCloudNodeClassSpec) { s.Bootstrap.BeforeJoin.Inline = "#!/bin/sh\nsysctl -w a=1\n" },
		"afterJoin":  func(s *HCloudNodeClassSpec) { s.Bootstrap.AfterJoin = &UserDataPart{Inline: "x"} },
		"join":       func(s *HCloudNodeClassSpec) { s.Bootstrap.Join = &UserDataPart{Inline: "x"} },
		"beforeJoin secretRef": func(s *HCloudNodeClassSpec) {
			s.Bootstrap.BeforeJoin = &UserDataPart{SecretRef: &SecretKeyReference{Namespace: "n", Name: "s", Key: "k"}}
		},
		"k0sVersion":             func(s *HCloudNodeClassSpec) { s.Bootstrap.K0sVersion = "v1.34.2+k0s.0" },
		"imageSelector version":  func(s *HCloudNodeClassSpec) { s.ImageSelector.Version = "14" },
		"imageSelector selector": func(s *HCloudNodeClassSpec) { s.ImageSelector.Selector = map[string]string{"role": "worker"} },
		"sshKeyIDs":              func(s *HCloudNodeClassSpec) { s.SSHKeyIDs = []int64{3} },
		"enablePublicIPv4":       func(s *HCloudNodeClassSpec) { s.EnablePublicIPv4 = &off },
		"enablePublicIPv6":       func(s *HCloudNodeClassSpec) { s.EnablePublicIPv6 = &off },
		"placementGroupStrategy": func(s *HCloudNodeClassSpec) { s.PlacementGroupStrategy = "none" },
	} {
		t.Run(name, func(t *testing.T) {
			nc := hashedNodeClass()
			before := nc.Hash()
			edit(&nc.Spec)
			if nc.Hash() == before {
				t.Errorf("hash unchanged after editing %s", name)
			}
		})
	}
}

func TestHash_IgnoresFieldsCheckedAgainstServerOrNotNeedingNewNodes(t *testing.T) {
	for name, edit := range map[string]func(*HCloudNodeClassSpec){
		"locations":   func(s *HCloudNodeClassSpec) { s.Locations = []string{"fsn1"} },
		"networkID":   func(s *HCloudNodeClassSpec) { s.NetworkID = 2 },
		"firewallIDs": func(s *HCloudNodeClassSpec) { s.FirewallIDs = []int64{8} },
		"labels":      func(s *HCloudNodeClassSpec) { s.Labels = map[string]string{"team": "b"} },
		"kubelet": func(s *HCloudNodeClassSpec) {
			s.Kubelet = &KubeletConfiguration{KubeReserved: map[string]ReservedQuantity{"cpu": "200m"}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			nc := hashedNodeClass()
			before := nc.Hash()
			edit(&nc.Spec)
			if nc.Hash() != before {
				t.Errorf("hash changed after editing %s", name)
			}
		})
	}
}

func TestHash_IgnoresSSHKeyOrder(t *testing.T) {
	nc := hashedNodeClass()
	before := nc.Hash()
	nc.Spec.SSHKeyIDs = []int64{4, 3}
	if nc.Hash() != before {
		t.Error("hash changed after reordering sshKeyIDs")
	}
}

func TestHash_UnsetPublicIPMatchesItsDefault(t *testing.T) {
	nc := hashedNodeClass()
	before := nc.Hash()
	nc.Spec.EnablePublicIPv4 = nil
	nc.Spec.EnablePublicIPv6 = nil
	if nc.Hash() != before {
		t.Error("unset enablePublicIPv4/IPv6 hashed differently from true")
	}
}
