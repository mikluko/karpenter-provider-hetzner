// Copyright The karpenter-provider-hetzner Authors.
// SPDX-License-Identifier: Apache-2.0

package operator

import (
	"os"
	"path/filepath"
	"testing"

	"k8s.io/client-go/rest"
)

func TestLoadConfig_RequiresClusterName(t *testing.T) {
	t.Setenv("CLUSTER_NAME", "")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("expected error when CLUSTER_NAME is unset")
	}
}

func TestLoadConfig_ReadsClusterName(t *testing.T) {
	t.Setenv("CLUSTER_NAME", "test-cluster")
	t.Setenv("API_SERVER_URL", "https://10.0.0.2:6443")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClusterName != "test-cluster" {
		t.Errorf("got cluster name %q, want test-cluster", cfg.ClusterName)
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

func TestLoadConfig_APIServerURL(t *testing.T) {
	t.Setenv("CLUSTER_NAME", "test-cluster")
	for _, tc := range []struct {
		url string
		ok  bool
	}{
		{"https://10.0.0.2:6443", true},
		{"https://api.example.com", true},
		{"", false},
		{"http://10.0.0.2:6443", false},
		{"10.0.0.2:6443", false},
		{"https://", false},
	} {
		t.Setenv("API_SERVER_URL", tc.url)
		cfg, err := LoadConfig()
		if (err == nil) != tc.ok {
			t.Errorf("API_SERVER_URL %q: err = %v, want ok=%v", tc.url, err, tc.ok)
			continue
		}
		if tc.ok && cfg.APIServerURL != tc.url {
			t.Errorf("APIServerURL = %q, want %q", cfg.APIServerURL, tc.url)
		}
	}
}

func TestClusterCA(t *testing.T) {
	if got, err := ClusterCA(&rest.Config{TLSClientConfig: rest.TLSClientConfig{CAData: []byte("inline")}}); err != nil || string(got) != "inline" {
		t.Errorf("CAData: got %q, %v", got, err)
	}
	file := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(file, []byte("from-file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := ClusterCA(&rest.Config{TLSClientConfig: rest.TLSClientConfig{CAFile: file}}); err != nil || string(got) != "from-file" {
		t.Errorf("CAFile: got %q, %v", got, err)
	}
	if _, err := ClusterCA(&rest.Config{}); err == nil {
		t.Error("expected an error without a CA")
	}
}
