// Copyright The karpenter-provider-hetzner Authors.
// SPDX-License-Identifier: Apache-2.0

package v1_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// startCRDClient installs the shipped CRDs on a real API server and returns a
// client for it. Needs envtest binaries; run via task test-envtest.
func startCRDClient(t *testing.T) client.Client {
	t.Helper()
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; run task test-envtest")
	}
	env := &envtest.Environment{
		CRDDirectoryPaths:     []string{"../../../charts/karpenter-provider-hetzner/crds"},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := env.Start()
	if err != nil {
		t.Fatalf("installing the CRDs: %v", err)
	}
	t.Cleanup(func() { _ = env.Stop() })
	c, err := client.New(cfg, client.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestCRD_KubeletAdmission checks spec.kubelet admission end to end: the schema
// must compile and fit the CEL cost budget, and the key rules must admit every
// kubelet signal while refusing anything else.
func TestCRD_KubeletAdmission(t *testing.T) {
	c := startCRDClient(t)

	n := 0
	create := func(kubelet map[string]any) error {
		n++
		return c.Create(context.Background(), &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "karpenter.hetzner.cloud/v1",
			"kind":       "HCloudNodeClass",
			"metadata":   map[string]any{"name": fmt.Sprintf("nc-%d", n)},
			"spec": map[string]any{
				"locations":     []any{"nbg1"},
				"networkID":     int64(1),
				"imageSelector": map[string]any{"family": "debian"},
				"kubelet":       kubelet,
			},
		}})
	}

	allSignals := map[string]any{}
	for _, s := range []string{"memory.available", "allocatableMemory.available", "nodefs.available", "nodefs.inodesFree",
		"imagefs.available", "imagefs.inodesFree", "containerfs.available", "containerfs.inodesFree", "pid.available"} {
		allSignals[s] = "10%"
	}
	for name, tc := range map[string]struct {
		kubelet map[string]any
		admit   bool
	}{
		"every kubelet eviction signal": {map[string]any{"evictionHard": allSignals}, true},
		"every reservation key": {map[string]any{
			"systemReserved": map[string]any{"cpu": "200m", "memory": "512Mi", "ephemeral-storage": "1Gi", "pid": "100"},
			"kubeReserved":   map[string]any{"cpu": "200m", "memory": "512Mi", "ephemeral-storage": "1Gi", "pid": "100"},
		}, true},
		"unknown eviction signal":  {map[string]any{"evictionHard": map[string]any{"memory.free": "1Gi"}}, false},
		"unknown reservation key":  {map[string]any{"kubeReserved": map[string]any{"gpu": "1"}}, false},
		"kubelet flag syntax":      {map[string]any{"evictionHard": map[string]any{"memory.available": "<400Mi"}}, false},
		"non-quantity reservation": {map[string]any{"systemReserved": map[string]any{"memory": "512MB"}}, false},
	} {
		t.Run(name, func(t *testing.T) {
			if err := create(tc.kubelet); (err == nil) != tc.admit {
				t.Errorf("admitted=%v, want %v (err: %v)", err == nil, tc.admit, err)
			}
		})
	}
}

// TestCRD_NetworkAdmission checks that networkID may be left out, and that a
// node class without one keeps at least one public address.
func TestCRD_NetworkAdmission(t *testing.T) {
	c := startCRDClient(t)

	n := 0
	for name, tc := range map[string]struct {
		spec  map[string]any
		admit bool
	}{
		"no network":                    {map[string]any{}, true},
		"no network, IPv6 only":         {map[string]any{"enablePublicIPv4": false}, true},
		"no network, no public address": {map[string]any{"enablePublicIPv4": false, "enablePublicIPv6": false}, false},
		"network, no public address":    {map[string]any{"networkID": int64(1), "enablePublicIPv4": false, "enablePublicIPv6": false}, true},
		"network ID zero":               {map[string]any{"networkID": int64(0)}, false},
	} {
		t.Run(name, func(t *testing.T) {
			n++
			spec := map[string]any{
				"locations":     []any{"nbg1"},
				"imageSelector": map[string]any{"family": "debian"},
			}
			for k, v := range tc.spec {
				spec[k] = v
			}
			err := c.Create(context.Background(), &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "karpenter.hetzner.cloud/v1",
				"kind":       "HCloudNodeClass",
				"metadata":   map[string]any{"name": fmt.Sprintf("net-%d", n)},
				"spec":       spec,
			}})
			if (err == nil) != tc.admit {
				t.Errorf("admitted=%v, want %v (err: %v)", err == nil, tc.admit, err)
			}
		})
	}
}
