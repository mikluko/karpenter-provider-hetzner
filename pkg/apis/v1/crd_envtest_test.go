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

var validBootstrap = map[string]any{"k0sVersion": "v1.34.1+k0s.0"}

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
				"bootstrap":     validBootstrap,
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
				"bootstrap":     validBootstrap,
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

// TestCRD_NodeNamesAdmission checks that nodeNames takes distinct hostnames only.
func TestCRD_NodeNamesAdmission(t *testing.T) {
	c := startCRDClient(t)

	n := 0
	for name, tc := range map[string]struct {
		names []any
		admit bool
	}{
		"hostnames":         {[]any{"alpha", "delta-two"}, true},
		"duplicate":         {[]any{"alpha", "alpha"}, false},
		"upper case":        {[]any{"Alpha"}, false},
		"dot":               {[]any{"alpha.sarak.sh"}, false},
		"leading hyphen":    {[]any{"-alpha"}, false},
		"longer than label": {[]any{"g" + fmt.Sprintf("%063d", 0)}, false},
	} {
		t.Run(name, func(t *testing.T) {
			n++
			err := c.Create(context.Background(), &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "karpenter.hetzner.cloud/v1",
				"kind":       "HCloudNodeClass",
				"metadata":   map[string]any{"name": fmt.Sprintf("names-%d", n)},
				"spec": map[string]any{
					"locations":     []any{"nbg1"},
					"networkID":     int64(1),
					"imageSelector": map[string]any{"family": "debian"},
					"bootstrap":     validBootstrap,
					"nodeNames":     tc.names,
				},
			}})
			if (err == nil) != tc.admit {
				t.Errorf("admitted=%v, want %v (err: %v)", err == nil, tc.admit, err)
			}
		})
	}
}

// TestCRD_BootstrapAdmission checks that the generated join needs a k0s
// version, and that each part is given exactly one way.
func TestCRD_BootstrapAdmission(t *testing.T) {
	c := startCRDClient(t)

	inline := map[string]any{"inline": "#!/bin/sh\n"}
	secretRef := map[string]any{"secretRef": map[string]any{"namespace": "kube-system", "name": "worker", "key": "join"}}
	n := 0
	for name, tc := range map[string]struct {
		bootstrap map[string]any
		admit     bool
	}{
		"k0s version":                 {map[string]any{"k0sVersion": "v1.34.1+k0s.0"}, true},
		"k0s version and parts":       {map[string]any{"k0sVersion": "v1.34.1+k0s.0", "beforeJoin": inline, "afterJoin": secretRef}, true},
		"join replaced":               {map[string]any{"join": secretRef}, true},
		"nothing":                     {map[string]any{}, false},
		"parts without join or k0s":   {map[string]any{"beforeJoin": inline}, false},
		"k0s version without build":   {map[string]any{"k0sVersion": "v1.34.1+k0s"}, false},
		"k0s version, shell injected": {map[string]any{"k0sVersion": "v1.34.1+k0s.0;reboot"}, false},
		"part both ways":              {map[string]any{"k0sVersion": "v1.34.1+k0s.0", "beforeJoin": map[string]any{"inline": "x", "secretRef": secretRef["secretRef"]}}, false},
		"part neither way":            {map[string]any{"k0sVersion": "v1.34.1+k0s.0", "afterJoin": map[string]any{}}, false},
		"part empty inline":           {map[string]any{"k0sVersion": "v1.34.1+k0s.0", "afterJoin": map[string]any{"inline": ""}}, false},
	} {
		t.Run(name, func(t *testing.T) {
			n++
			err := c.Create(context.Background(), &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "karpenter.hetzner.cloud/v1",
				"kind":       "HCloudNodeClass",
				"metadata":   map[string]any{"name": fmt.Sprintf("bootstrap-%d", n)},
				"spec": map[string]any{
					"locations":     []any{"nbg1"},
					"networkID":     int64(1),
					"imageSelector": map[string]any{"family": "debian"},
					"bootstrap":     tc.bootstrap,
				},
			}})
			if (err == nil) != tc.admit {
				t.Errorf("admitted=%v, want %v (err: %v)", err == nil, tc.admit, err)
			}
		})
	}
}

// TestCRD_ImageSelectorAdmission checks that a snapshot node class names its
// snapshots by label or by ID, and brings its own join without a k0s version.
func TestCRD_ImageSelectorAdmission(t *testing.T) {
	c := startCRDClient(t)

	byLabel := map[string]any{"family": "snapshot", "selector": map[string]any{"role": "k0s-worker"}}
	byID := map[string]any{"family": "snapshot", "ids": []any{int64(460469663), int64(460469651)}}
	join := map[string]any{"join": map[string]any{"inline": "#!/bin/sh\n"}}
	joinAndVersion := map[string]any{"join": map[string]any{"inline": "#!/bin/sh\n"}, "k0sVersion": "v1.34.1+k0s.0"}
	n := 0
	for name, tc := range map[string]struct {
		imageSelector map[string]any
		bootstrap     map[string]any
		admit         bool
	}{
		"debian":                      {map[string]any{"family": "debian"}, validBootstrap, true},
		"debian, no k0s version":      {map[string]any{"family": "debian"}, map[string]any{}, false},
		"debian with ids":             {map[string]any{"family": "debian", "ids": []any{int64(1)}}, validBootstrap, false},
		"snapshot by label":           {byLabel, join, true},
		"snapshot by id":              {byID, join, true},
		"snapshot by label and id":    {map[string]any{"family": "snapshot", "selector": map[string]any{"role": "k0s-worker"}, "ids": []any{int64(1)}}, join, false},
		"snapshot by id with version": {map[string]any{"family": "snapshot", "ids": []any{int64(1)}, "version": "26.05"}, join, false},
		"snapshot, id zero":           {map[string]any{"family": "snapshot", "ids": []any{int64(0)}}, join, false},
		"snapshot, neither":           {map[string]any{"family": "snapshot"}, join, false},
		"snapshot, empty selector":    {map[string]any{"family": "snapshot", "selector": map[string]any{}}, join, false},
		"snapshot without join":       {byLabel, map[string]any{}, false},
		"snapshot with k0s version":   {byLabel, joinAndVersion, false},
		"snapshot, only k0s version":  {byLabel, validBootstrap, false},
		"unknown family":              {map[string]any{"family": "ubuntu"}, validBootstrap, false},
	} {
		t.Run(name, func(t *testing.T) {
			n++
			err := c.Create(context.Background(), &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "karpenter.hetzner.cloud/v1",
				"kind":       "HCloudNodeClass",
				"metadata":   map[string]any{"name": fmt.Sprintf("image-%d", n)},
				"spec": map[string]any{
					"locations":     []any{"nbg1"},
					"networkID":     int64(1),
					"imageSelector": tc.imageSelector,
					"bootstrap":     tc.bootstrap,
				},
			}})
			if (err == nil) != tc.admit {
				t.Errorf("admitted=%v, want %v (err: %v)", err == nil, tc.admit, err)
			}
		})
	}
}
