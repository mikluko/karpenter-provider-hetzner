// Copyright The karpenter-provider-hetzner Authors.
// SPDX-License-Identifier: Apache-2.0

package v1

import (
	"fmt"

	"github.com/mitchellh/hashstructure/v2"
)

const (
	// AnnotationHCloudNodeClassHash is the Hash of the node class a NodeClaim
	// was launched from.
	AnnotationHCloudNodeClassHash = Group + "/hcloudnodeclass-hash"
	// AnnotationHCloudNodeClassHashVersion is the HCloudNodeClassHashVersion the
	// NodeClaim's AnnotationHCloudNodeClassHash was computed under.
	AnnotationHCloudNodeClassHashVersion = Group + "/hcloudnodeclass-hash-version"

	// HCloudNodeClassHashVersion must be bumped whenever a change to Hash would
	// alter the hash of an unedited node class, so existing NodeClaims are
	// restamped rather than drifted on upgrade.
	HCloudNodeClassHashVersion = "v1"
)

// Hash returns a hash of the spec fields that change what a new server is
// launched with and cannot be compared against a running one. Fields tagged
// hash:"ignore" are either checked against the server by drift detection or
// take effect without a new server.
func (in *HCloudNodeClass) Hash() string {
	spec := in.Spec
	ipv4, ipv6 := spec.PublicIPv4Enabled(), spec.PublicIPv6Enabled()
	spec.EnablePublicIPv4, spec.EnablePublicIPv6 = &ipv4, &ipv6
	h, err := hashstructure.Hash(spec, hashstructure.FormatV2, &hashstructure.HashOptions{
		SlicesAsSets:    true,
		IgnoreZeroValue: true,
		ZeroNil:         true,
	})
	if err != nil {
		panic(fmt.Sprintf("hashing HCloudNodeClass spec: %v", err))
	}
	return fmt.Sprint(h)
}
