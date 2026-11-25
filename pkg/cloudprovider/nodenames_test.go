// Copyright The karpenter-provider-hetzner Authors.
// SPDX-License-Identifier: Apache-2.0

package cloudprovider

import (
	"testing"

	"k8s.io/apimachinery/pkg/util/sets"
)

func TestRenderNodeNames_EachTemplateDrawsItsOwnRand(t *testing.T) {
	names, err := renderNodeNames([]string{"n{{ .Rand }}", "n{{ .Rand }}"}, nodeNameData{})
	if err != nil {
		t.Fatal(err)
	}
	if names[0] == names[1] {
		t.Errorf("both templates rendered %q", names[0])
	}
}

func TestNodeNameHolds_HeldNameIsNotPickedAgain(t *testing.T) {
	var h nodeNameHolds
	pool := []string{"alpha", "beta"}

	first, ok := h.hold(pool, sets.New[string]())
	if !ok {
		t.Fatal("first hold found no name")
	}
	second, ok := h.hold(pool, sets.New[string]())
	if !ok || second == first {
		t.Fatalf("second hold = %q, %v; want the other name than %q", second, ok, first)
	}
	if name, ok := h.hold(pool, sets.New[string]()); ok {
		t.Fatalf("third hold = %q, want none", name)
	}

	h.release(first)
	if name, ok := h.hold(pool, sets.New[string]()); !ok || name != first {
		t.Fatalf("hold after release = %q, %v; want %q", name, ok, first)
	}
}

func TestNodeNameHolds_PicksAtRandom(t *testing.T) {
	pool := []string{"alpha", "beta", "gamma"}
	seen := sets.New[string]()
	for range 300 {
		var h nodeNameHolds
		name, ok := h.hold(pool, sets.New("gamma"))
		if !ok {
			t.Fatal("hold found no name")
		}
		seen.Insert(name)
	}
	if want := sets.New("alpha", "beta"); !seen.Equal(want) {
		t.Errorf("picked %v, want every free name and only those: %v", sets.List(seen), sets.List(want))
	}
}
