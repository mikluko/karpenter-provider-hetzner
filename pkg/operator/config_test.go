// Copyright The karpenter-provider-hetzner Authors.
// SPDX-License-Identifier: Apache-2.0

package operator

import "testing"

func TestLoadConfig_RequiresClusterName(t *testing.T) {
	t.Setenv("CLUSTER_NAME", "")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("expected error when CLUSTER_NAME is unset")
	}
}

func TestLoadConfig_ReadsClusterName(t *testing.T) {
	t.Setenv("CLUSTER_NAME", "tugarin")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClusterName != "tugarin" {
		t.Errorf("got cluster name %q, want tugarin", cfg.ClusterName)
	}
}

func TestLoadConfig_RejectsInvalidClusterName(t *testing.T) {
	for _, bad := range []string{"has space", "slash/name", "comma,name", "töö"} {
		t.Setenv("CLUSTER_NAME", bad)
		if _, err := LoadConfig(); err == nil {
			t.Errorf("expected error for invalid cluster name %q", bad)
		}
	}
}
