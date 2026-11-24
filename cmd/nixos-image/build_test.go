// Copyright The karpenter-provider-hetzner Authors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"slices"
	"testing"
	"time"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

var testNow = time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)

// fakeCloud records the calls a build makes. failCreate holds the error each
// successive CreateServer call returns; a nil entry, or none, succeeds.
type fakeCloud struct {
	calls      []string
	types      []*hcloud.ServerType
	failCreate []error
	failOn     string
	created    []string
	publicNet  *hcloud.ServerCreatePublicNet
	snapshot   struct {
		description string
		labels      map[string]string
	}
}

func (f *fakeCloud) call(name string) error {
	f.calls = append(f.calls, name)
	if f.failOn == name {
		return errors.New(name + " failed")
	}
	return nil
}

func (f *fakeCloud) ServerTypes(context.Context) ([]*hcloud.ServerType, error) {
	return f.types, f.call("ServerTypes")
}

func (f *fakeCloud) CreateSSHKey(_ context.Context, name, _ string, _ map[string]string) (*hcloud.SSHKey, error) {
	return &hcloud.SSHKey{Name: name}, f.call("CreateSSHKey")
}

func (f *fakeCloud) DeleteSSHKey(context.Context, *hcloud.SSHKey) error {
	return f.call("DeleteSSHKey")
}

func (f *fakeCloud) CreateServer(_ context.Context, opts hcloud.ServerCreateOpts) (*hcloud.Server, error) {
	f.created = append(f.created, opts.ServerType.Name)
	f.publicNet = opts.PublicNet
	if err := f.call("CreateServer"); err != nil {
		return nil, err
	}
	if len(f.failCreate) > 0 {
		err := f.failCreate[0]
		f.failCreate = f.failCreate[1:]
		if err != nil {
			return nil, err
		}
	}
	return &hcloud.Server{
		Name:       opts.Name,
		ServerType: opts.ServerType,
		PublicNet:  hcloud.ServerPublicNet{IPv4: hcloud.ServerPublicNetIPv4{IP: net.ParseIP("192.0.2.10")}},
	}, nil
}

func (f *fakeCloud) PowerOff(context.Context, *hcloud.Server) error { return f.call("PowerOff") }

func (f *fakeCloud) Snapshot(_ context.Context, _ *hcloud.Server, description string, labels map[string]string) (*hcloud.Image, error) {
	f.snapshot.description, f.snapshot.labels = description, labels
	if err := f.call("Snapshot"); err != nil {
		return nil, err
	}
	return &hcloud.Image{ID: 42}, nil
}

func (f *fakeCloud) DeleteServer(context.Context, *hcloud.Server) error {
	return f.call("DeleteServer")
}

type fakeTools struct {
	calls    []string
	failOn   string
	kexecURL string
}

func (f *fakeTools) call(name string) error {
	f.calls = append(f.calls, name)
	if f.failOn == name {
		return errors.New(name + " failed")
	}
	return nil
}

func (f *fakeTools) Describe(context.Context, string, string) (imageInfo, error) {
	return imageInfo{NixOS: "26.05", K0s: "1.36.6+k0s.0", Rev: "abc123"}, f.call("Describe")
}

func (f *fakeTools) KeyPair(_ context.Context, dir string) (string, string, error) {
	return dir + "/id_ed25519", "ssh-ed25519 AAAA nixos-image", f.call("KeyPair")
}

func (f *fakeTools) WaitSSH(context.Context, string, string) error { return f.call("WaitSSH") }

func (f *fakeTools) Install(_ context.Context, _, _, kexecURL, _, _ string) error {
	f.kexecURL = kexecURL
	return f.call("Install")
}

func (f *fakeTools) Unmount(context.Context, string, string) error { return f.call("Unmount") }

func priced(name string, arch hcloud.Architecture, disk int, location, net string) *hcloud.ServerType {
	return &hcloud.ServerType{
		Name:         name,
		Architecture: arch,
		Disk:         disk,
		Memory:       4,
		Pricings:     []hcloud.ServerTypeLocationPricing{{Location: &hcloud.Location{Name: location}, Hourly: hcloud.Price{Net: net}}},
	}
}

func testBuild(t *testing.T, c *fakeCloud, tl *fakeTools, cfg buildConfig) (*hcloud.Image, error) {
	t.Helper()
	if c.types == nil {
		c.types = []*hcloud.ServerType{priced("cx23", hcloud.ArchitectureX86, 40, "nbg1", "0.005")}
	}
	if cfg.Arch == "" {
		cfg.Arch = hcloud.ArchitectureX86
	}
	if cfg.K0sMinor == "" {
		cfg.K0sMinor = "1.36"
	}
	if cfg.Location == "" {
		cfg.Location = "nbg1"
	}
	b := &builder{cloud: c, tools: tl, log: slog.New(slog.DiscardHandler), now: func() time.Time { return testNow }}
	return b.build(t.Context(), cfg)
}

func TestBuild_SnapshotsAndCleansUp(t *testing.T) {
	c, tl := &fakeCloud{}, &fakeTools{}
	img, err := testBuild(t, c, tl, buildConfig{Labels: map[string]string{"role": "edge", "team": "infra"}})
	if err != nil {
		t.Fatal(err)
	}
	if img.ID != 42 {
		t.Errorf("image ID = %d, want 42", img.ID)
	}
	wantCloud := []string{"ServerTypes", "CreateSSHKey", "CreateServer", "PowerOff", "Snapshot", "DeleteServer", "DeleteSSHKey"}
	if !slices.Equal(c.calls, wantCloud) {
		t.Errorf("cloud calls = %v, want %v", c.calls, wantCloud)
	}
	if n := c.publicNet; n == nil || !n.EnableIPv4 || n.EnableIPv6 {
		t.Errorf("public net = %+v, want IPv4 only: one primary IP per builder", n)
	}
	wantTools := []string{"Describe", "KeyPair", "WaitSSH", "Install", "Unmount"}
	if !slices.Equal(tl.calls, wantTools) {
		t.Errorf("tool calls = %v, want %v", tl.calls, wantTools)
	}
	if got, want := tl.kexecURL, "https://github.com/nix-community/nixos-images/releases/download/nixos-26.05/nixos-kexec-installer-noninteractive-x86_64-linux.tar.gz"; got != want {
		t.Errorf("kexec URL = %q, want %q", got, want)
	}
	if got, want := c.snapshot.description, "nixos-26.05 k0s-v1.36.6+k0s.0"; got != want {
		t.Errorf("description = %q, want %q", got, want)
	}
	wantLabels := map[string]string{"role": "edge", "team": "infra", "nixos": "26.05", "k0s-minor": "1.36", "rev": "abc123"}
	for k, v := range wantLabels {
		if c.snapshot.labels[k] != v {
			t.Errorf("label %s = %q, want %q", k, c.snapshot.labels[k], v)
		}
	}
}

// TestBuild_FailureStillCleansUp pins that a failure after the server exists
// deletes the server and the key, and reports the failure.
func TestBuild_FailureStillCleansUp(t *testing.T) {
	for _, step := range []string{"WaitSSH", "Install", "Unmount"} {
		t.Run(step, func(t *testing.T) {
			c, tl := &fakeCloud{}, &fakeTools{failOn: step}
			if _, err := testBuild(t, c, tl, buildConfig{}); err == nil {
				t.Fatal("want an error")
			}
			if !slices.Contains(c.calls, "DeleteServer") || !slices.Contains(c.calls, "DeleteSSHKey") {
				t.Errorf("cloud calls = %v, want the server and key deleted", c.calls)
			}
			if slices.Contains(c.calls, "Snapshot") {
				t.Errorf("cloud calls = %v, want no snapshot", c.calls)
			}
		})
	}
}

func TestBuild_KeepLeavesFailedServer(t *testing.T) {
	c, tl := &fakeCloud{}, &fakeTools{failOn: "Install"}
	if _, err := testBuild(t, c, tl, buildConfig{Keep: true}); err == nil {
		t.Fatal("want an error")
	}
	if slices.Contains(c.calls, "DeleteServer") {
		t.Errorf("cloud calls = %v, want the server kept", c.calls)
	}
	if !slices.Contains(c.calls, "DeleteSSHKey") {
		t.Errorf("cloud calls = %v, want the key deleted", c.calls)
	}
}

func TestBuild_CleanupErrorIsReported(t *testing.T) {
	c, tl := &fakeCloud{failOn: "DeleteServer"}, &fakeTools{}
	if _, err := testBuild(t, c, tl, buildConfig{}); err == nil {
		t.Fatal("want the failed delete reported")
	}
	if !slices.Contains(c.calls, "DeleteSSHKey") {
		t.Errorf("cloud calls = %v, want the key deleted after a failed server delete", c.calls)
	}
}

func TestBuild_UnavailableTypeFallsThrough(t *testing.T) {
	c := &fakeCloud{
		types: []*hcloud.ServerType{
			priced("cx23", hcloud.ArchitectureX86, 40, "nbg1", "0.005"),
			priced("cpx22", hcloud.ArchitectureX86, 40, "nbg1", "0.008"),
		},
		failCreate: []error{hcloud.Error{Code: hcloud.ErrorCodeResourceUnavailable}},
	}
	if _, err := testBuild(t, c, &fakeTools{}, buildConfig{}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"cx23", "cpx22"}; !slices.Equal(c.created, want) {
		t.Errorf("tried %v, want %v", c.created, want)
	}
}

func TestBuild_OtherCreateErrorStops(t *testing.T) {
	c := &fakeCloud{
		types: []*hcloud.ServerType{
			priced("cx23", hcloud.ArchitectureX86, 40, "nbg1", "0.005"),
			priced("cpx22", hcloud.ArchitectureX86, 40, "nbg1", "0.008"),
		},
		failCreate: []error{hcloud.Error{Code: hcloud.ErrorCodeInvalidInput}},
	}
	if _, err := testBuild(t, c, &fakeTools{}, buildConfig{}); err == nil {
		t.Fatal("want an error")
	}
	if want := []string{"cx23"}; !slices.Equal(c.created, want) {
		t.Errorf("tried %v, want %v", c.created, want)
	}
	if !slices.Contains(c.calls, "DeleteSSHKey") {
		t.Errorf("cloud calls = %v, want the key deleted", c.calls)
	}
}

func TestBuild_ExplicitServerTypeSkipsCatalogue(t *testing.T) {
	c := &fakeCloud{}
	if _, err := testBuild(t, c, &fakeTools{}, buildConfig{ServerType: "cax21"}); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(c.calls, "ServerTypes") {
		t.Errorf("cloud calls = %v, want no catalogue read", c.calls)
	}
	if want := []string{"cax21"}; !slices.Equal(c.created, want) {
		t.Errorf("tried %v, want %v", c.created, want)
	}
}

func TestBuilderTypes(t *testing.T) {
	retired := priced("cpx11", hcloud.ArchitectureX86, 40, "nbg1", "0.001")
	retired.Locations = []hcloud.ServerTypeLocation{{
		Location: &hcloud.Location{Name: "nbg1"},
		Deprecation: &hcloud.DeprecationInfo{
			Announced:        testNow.Add(-60 * 24 * time.Hour),
			UnavailableAfter: testNow.Add(-24 * time.Hour),
		},
	}}
	tiny := priced("cpx02", hcloud.ArchitectureX86, 20, "nbg1", "0.001")
	tiny.Memory = 1
	types := []*hcloud.ServerType{
		tiny,
		priced("cpx22", hcloud.ArchitectureX86, 40, "nbg1", "0.008"),
		priced("cx33", hcloud.ArchitectureX86, 80, "nbg1", "0.004"),
		priced("cx23", hcloud.ArchitectureX86, 40, "nbg1", "0.005"),
		priced("cax11", hcloud.ArchitectureARM, 40, "nbg1", "0.004"),
		priced("ccx13", hcloud.ArchitectureX86, 40, "fsn1", "0.002"),
		priced("cx22", hcloud.ArchitectureX86, 40, "nbg1", "unpriced"),
		retired,
	}
	var got []string
	for _, st := range builderTypes(types, hcloud.ArchitectureX86, "nbg1", testNow) {
		got = append(got, st.Name)
	}
	if want := []string{"cx23", "cpx22"}; !slices.Equal(got, want) {
		t.Errorf("builderTypes = %v, want %v", got, want)
	}
}

func TestParseArches(t *testing.T) {
	for name, tc := range map[string]struct {
		in      string
		want    []hcloud.Architecture
		wantErr bool
	}{
		"both":       {"x86,arm", []hcloud.Architecture{hcloud.ArchitectureX86, hcloud.ArchitectureARM}, false},
		"one":        {"arm", []hcloud.Architecture{hcloud.ArchitectureARM}, false},
		"repeated":   {"arm, arm", []hcloud.Architecture{hcloud.ArchitectureARM}, false},
		"unknown":    {"x86,riscv", nil, true},
		"empty item": {"x86,", nil, true},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := parseArches(tc.in)
			if (err != nil) != tc.wantErr || !slices.Equal(got, tc.want) {
				t.Errorf("parseArches(%q) = %v, %v; want %v, error %v", tc.in, got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestKexecURL_Arm(t *testing.T) {
	want := "https://github.com/nix-community/nixos-images/releases/download/nixos-26.05/nixos-kexec-installer-noninteractive-aarch64-linux.tar.gz"
	if got := kexecURL(hcloud.ArchitectureARM, "26.05"); got != want {
		t.Errorf("kexecURL = %q, want %q", got, want)
	}
}

func TestWorkerConfig(t *testing.T) {
	if got, want := workerConfig(hcloud.ArchitectureARM, "1.36"), "k0s_1_36-arm"; got != want {
		t.Errorf("workerConfig = %q, want %q", got, want)
	}
}

func TestFlakeRevision(t *testing.T) {
	for name, tc := range map[string]struct {
		meta string
		want string
	}{
		"clean": {`{"revision":"abc","locked":{"rev":"abc"}}`, "abc"},
		"dirty": {`{"dirtyRevision":"abc-dirty"}`, "abc-dirty"},
		"none":  {`{"description":"x"}`, ""},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := flakeRevision([]byte(tc.meta))
			if (err != nil) != (tc.want == "") || got != tc.want {
				t.Errorf("flakeRevision = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}
