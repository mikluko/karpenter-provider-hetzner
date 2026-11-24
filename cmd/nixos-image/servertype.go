// Copyright The karpenter-provider-hetzner Authors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"cmp"
	"slices"
	"strconv"
	"time"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// minBuilderMemory is the RAM, in GB, a builder needs: nixos-anywhere asks for
// 1.5 GB to kexec into its installer, and the build runs there too.
const minBuilderMemory = 2

// builderTypes returns the server types of arch on sale in location, with at
// least minBuilderMemory, that share the smallest disk, cheapest first. A
// snapshot only boots on a server type whose disk is at least as large as its
// builder's.
func builderTypes(types []*hcloud.ServerType, arch hcloud.Architecture, location string, now time.Time) []*hcloud.ServerType {
	type candidate struct {
		st    *hcloud.ServerType
		price float64
	}
	var cs []candidate
	for _, st := range types {
		if st.Architecture != arch || st.Memory < minBuilderMemory || retiredIn(st, location, now) {
			continue
		}
		if price, ok := hourlyNet(st, location); ok {
			cs = append(cs, candidate{st, price})
		}
	}
	if len(cs) == 0 {
		return nil
	}
	minDisk := slices.MinFunc(cs, func(a, b candidate) int { return cmp.Compare(a.st.Disk, b.st.Disk) }).st.Disk
	cs = slices.DeleteFunc(cs, func(c candidate) bool { return c.st.Disk != minDisk })
	slices.SortStableFunc(cs, func(a, b candidate) int { return cmp.Compare(a.price, b.price) })
	out := make([]*hcloud.ServerType, len(cs))
	for i, c := range cs {
		out[i] = c.st
	}
	return out
}

// retiredIn reports whether hcloud has stopped creating st in location: its
// deprecation there has passed the unavailable-after date.
func retiredIn(st *hcloud.ServerType, location string, now time.Time) bool {
	for _, l := range st.Locations {
		if l.Location != nil && l.Location.Name == location && l.IsDeprecated() && now.After(l.UnavailableAfter()) {
			return true
		}
	}
	return false
}

// hourlyNet returns st's net hourly price in location, and false when st is
// not priced there.
func hourlyNet(st *hcloud.ServerType, location string) (float64, bool) {
	for _, p := range st.Pricings {
		if p.Location == nil || p.Location.Name != location {
			continue
		}
		v, err := strconv.ParseFloat(p.Hourly.Net, 64)
		return v, err == nil
	}
	return 0, false
}
