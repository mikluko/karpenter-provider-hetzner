// Copyright The karpenter-provider-hetzner Authors.
// SPDX-License-Identifier: Apache-2.0

package imagefamily

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"

	apiv1 "github.com/mikluko/karpenter-provider-hetzner/pkg/apis/v1"
)

// mockImageClient is a fake ImageClient for testing.
type mockImageClient struct {
	images   []*hcloud.Image
	lastOpts hcloud.ImageListOpts
}

func (m *mockImageClient) AllWithOpts(_ context.Context, opts hcloud.ImageListOpts) ([]*hcloud.Image, error) {
	m.lastOpts = opts
	var result []*hcloud.Image
	for _, img := range m.images {
		// Filter by type if specified.
		if len(opts.Type) > 0 {
			match := false
			for _, t := range opts.Type {
				if img.Type == t {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		// Filter by architecture if specified.
		if len(opts.Architecture) > 0 {
			match := false
			for _, a := range opts.Architecture {
				if img.Architecture == a {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		result = append(result, img)
	}
	return result, nil
}

func makeImage(id int64, imgType hcloud.ImageType, arch hcloud.Architecture, description string, created time.Time) *hcloud.Image {
	return &hcloud.Image{
		ID:           id,
		Type:         imgType,
		Architecture: arch,
		Description:  description,
		Created:      created,
	}
}

var baseTime = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

func TestResolveDebian_PicksNewest(t *testing.T) {
	images := []*hcloud.Image{
		makeImage(1, hcloud.ImageTypeSystem, hcloud.ArchitectureX86, "Debian 11", baseTime),
		makeImage(2, hcloud.ImageTypeSystem, hcloud.ArchitectureX86, "Debian 13", baseTime.Add(48*time.Hour)),
		makeImage(3, hcloud.ImageTypeSystem, hcloud.ArchitectureX86, "Debian 12", baseTime.Add(24*time.Hour)),
		makeImage(4, hcloud.ImageTypeSystem, hcloud.ArchitectureX86, "Ubuntu 24.04", baseTime.Add(72*time.Hour)),
	}
	p := NewProvider(&mockImageClient{images: images})

	img, err := p.Resolve(context.Background(), apiv1.ImageSelector{Family: "debian"}, hcloud.ArchitectureX86)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if img.ID != 2 {
		t.Errorf("expected newest Debian (ID=2), got ID=%d", img.ID)
	}
}

func TestResolveDebian_TieKeepsFirstListed(t *testing.T) {
	images := []*hcloud.Image{
		makeImage(1, hcloud.ImageTypeSystem, hcloud.ArchitectureX86, "Debian 13", baseTime),
		makeImage(2, hcloud.ImageTypeSystem, hcloud.ArchitectureX86, "Debian 12", baseTime),
	}
	p := NewProvider(&mockImageClient{images: images})

	img, err := p.Resolve(context.Background(), apiv1.ImageSelector{Family: "debian"}, hcloud.ArchitectureX86)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if img.ID != 1 {
		t.Errorf("expected first listed of equally new images (ID=1), got ID=%d", img.ID)
	}
}

func TestResolveDebian_VersionPin(t *testing.T) {
	images := []*hcloud.Image{
		makeImage(1, hcloud.ImageTypeSystem, hcloud.ArchitectureARM, "Debian 12", baseTime),
		makeImage(2, hcloud.ImageTypeSystem, hcloud.ArchitectureARM, "Debian 13", baseTime.Add(24*time.Hour)),
	}
	p := NewProvider(&mockImageClient{images: images})

	img, err := p.Resolve(context.Background(), apiv1.ImageSelector{Family: "debian", Version: "12"}, hcloud.ArchitectureARM)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if img.ID != 1 {
		t.Errorf("expected Debian 12 (ID=1), got ID=%d", img.ID)
	}
}

func TestResolveDebian_ArchFilter(t *testing.T) {
	images := []*hcloud.Image{
		makeImage(1, hcloud.ImageTypeSystem, hcloud.ArchitectureARM, "Debian 13", baseTime),
		makeImage(2, hcloud.ImageTypeSystem, hcloud.ArchitectureX86, "Debian 13", baseTime),
	}
	p := NewProvider(&mockImageClient{images: images})

	img, err := p.Resolve(context.Background(), apiv1.ImageSelector{Family: "debian"}, hcloud.ArchitectureARM)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if img.ID != 1 {
		t.Errorf("expected ARM image (ID=1), got ID=%d", img.ID)
	}
}

func TestResolveDebian_NoMatchIsPermanent(t *testing.T) {
	images := []*hcloud.Image{
		makeImage(1, hcloud.ImageTypeSystem, hcloud.ArchitectureX86, "Debian 12", baseTime),
		makeImage(2, hcloud.ImageTypeSystem, hcloud.ArchitectureX86, "Ubuntu 24.04", baseTime),
	}
	p := NewProvider(&mockImageClient{images: images})

	_, err := p.Resolve(context.Background(), apiv1.ImageSelector{Family: "debian", Version: "13"}, hcloud.ArchitectureX86)
	if !IsPermanent(err) {
		t.Fatalf("expected a permanent error, got %v", err)
	}
}

func TestResolveDebian_LabelSelectorForwarded(t *testing.T) {
	fc := &mockImageClient{images: []*hcloud.Image{
		makeImage(2, hcloud.ImageTypeSystem, hcloud.ArchitectureX86, "Debian 13", baseTime),
	}}
	p := NewProvider(fc)
	sel := apiv1.ImageSelector{Family: "debian", Selector: map[string]string{"role": "worker"}}
	if _, err := p.Resolve(context.Background(), sel, hcloud.ArchitectureX86); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := fc.lastOpts.LabelSelector; got != "role=worker" {
		t.Fatalf("label selector not forwarded: got %q", got)
	}
}

func TestResolveDebian_LabelSelectorFallsBackToNewestSnapshot(t *testing.T) {
	fc := &mockImageClient{images: []*hcloud.Image{
		makeImage(1, hcloud.ImageTypeSnapshot, hcloud.ArchitectureX86, "k8s-node-v1.36.4-old", baseTime),
		makeImage(2, hcloud.ImageTypeSnapshot, hcloud.ArchitectureX86, "k8s-node-v1.36.4-new", baseTime.Add(time.Hour)),
	}}
	p := NewProvider(fc)
	sel := apiv1.ImageSelector{Family: "debian", Selector: map[string]string{"role": "k8s-node"}}
	img, err := p.Resolve(context.Background(), sel, hcloud.ArchitectureX86)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if img.ID != 2 {
		t.Errorf("expected newest snapshot (ID=2), got ID=%d", img.ID)
	}
	if got := fc.lastOpts.LabelSelector; got != "role=k8s-node" {
		t.Fatalf("label selector not forwarded to snapshot lookup: got %q", got)
	}
}

func TestResolveDebian_NoSelectorIgnoresSnapshots(t *testing.T) {
	p := NewProvider(&mockImageClient{images: []*hcloud.Image{
		makeImage(1, hcloud.ImageTypeSnapshot, hcloud.ArchitectureX86, "debian snapshot", baseTime),
	}})
	if _, err := p.Resolve(context.Background(), apiv1.ImageSelector{Family: "debian"}, hcloud.ArchitectureX86); err == nil {
		t.Fatal("expected no match: snapshots need a label selector")
	}
}

// erroringImages fails every list call, standing in for a 429/5xx from hcloud.
type erroringImages struct{ err error }

func (e erroringImages) AllWithOpts(_ context.Context, _ hcloud.ImageListOpts) ([]*hcloud.Image, error) {
	return nil, e.err
}

// TestResolve_DistinguishesNotFoundFromAPIFailure pins the distinction the nodeclass
// controller depends on. An unreadable catalogue is not evidence that an image is
// absent: treating it as such clears a resolved architecture from NodeClass status, and
// instance-type selection then refuses that architecture, so Karpenter deletes
// NodeClaims over an API blip.
func TestResolve_DistinguishesNotFoundFromAPIFailure(t *testing.T) {
	sel := apiv1.ImageSelector{Family: "debian"}

	// Catalogue readable, genuinely empty -> definitive.
	p := NewProvider(&mockImageClient{})
	_, err := p.Resolve(context.Background(), sel, hcloud.ArchitectureX86)
	if err == nil {
		t.Fatal("expected an error for an empty catalogue")
	}
	if !IsPermanent(err) {
		t.Errorf("empty catalogue should be permanent, got %T: %v", err, err)
	}

	// Catalogue unreadable -> must NOT look definitive.
	p = NewProvider(erroringImages{err: errors.New("rate limited (429)")})
	_, err = p.Resolve(context.Background(), sel, hcloud.ArchitectureX86)
	if err == nil {
		t.Fatal("expected an error when the list call fails")
	}
	if IsPermanent(err) {
		t.Errorf("an API failure must not be reported as permanent: %v", err)
	}
}

// TestResolve_UnsupportedFamilyIsPermanent verifies that a family the provider cannot
// handle is classified permanent, not transient. Retrying never changes the answer, and
// the transient class is load-bearing: it makes the nodeclass controller preserve the
// previous image IDs and keep the condition green, so misclassifying a definitively bad
// spec here means launching stale images indefinitely with nothing reported.
func TestResolve_UnsupportedFamilyIsPermanent(t *testing.T) {
	images := []*hcloud.Image{
		makeImage(1, hcloud.ImageTypeSystem, hcloud.ArchitectureX86, "Ubuntu 24.04", baseTime),
		makeImage(2, hcloud.ImageTypeSnapshot, hcloud.ArchitectureX86, "talos v1.13.3", baseTime),
	}
	for _, family := range []string{"ubuntu", "talos", "windows"} {
		p := NewProvider(&mockImageClient{images: images})
		_, err := p.Resolve(context.Background(), apiv1.ImageSelector{Family: family}, hcloud.ArchitectureX86)
		if !IsPermanent(err) {
			t.Errorf("family %q: want a permanent error, got %v", family, err)
		}
	}
}
