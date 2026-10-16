# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/2.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.4.0] - 2026-10-16

### Changed

- `HCloudNodeClass.spec.networkID` is optional. Without it, servers attach to no private network, `NetworkReady` is True, and locations are not checked against network zones; `enablePublicIPv4` or `enablePublicIPv6` must then stay `true`. The `HCloudNodeClass` CRD changes, so apply it before `helm upgrade`, as the README describes.

## [0.3.0] - 2026-10-16

### Added

- NodePool disruption budgets take the `Unhealthy` reason, and `spec.disruption.consolidateAfter` defaults to `0s`.

### Changed

- Built with Karpenter core 1.15.0. The `NodePool` and `NodeClaim` CRDs change, so apply them before `helm upgrade`, as the README describes.

## [0.2.0] - 2026-10-16

### Added

- The chart is signed with cosign, as the image already was.

### Changed

- Release images and charts are signed with the certificate identity `https://github.com/mikluko/karpenter-provider-hetzner/.github/workflows/release-roll.yaml@refs/tags/v<version>`.
- Dependencies updated: golang 1.27.2.

### Security

- GHSA-gcjh-h69q-9w9g: JSON private fields could be exposed via NativeTypes and ParseStructTag, fixed by github.com/google/cel-go 0.30.0.

## [0.1.0] - 2026-10-15

The first release, taken from upstream karpenter-provider-hetzner 3.0.1. Its
history is in the [upstream changelog](https://github.com/mikluko/karpenter-provider-hetzner/blob/main/CHANGELOG.upstream.md).

### Added

- The `debian` image family resolves Hetzner's Debian images, the newest unless `imageSelector.version` pins one, for example `13`.
- `HCloudNodeClass.spec.kubelet` declares the reservations the node's bootstrap applies (`systemReserved`, `kubeReserved`, `evictionHard`), and Karpenter subtracts them when computing allocatable.
- A NodeClass without `spec.kubelet` keeps the flat 100m CPU and 100Mi memory `kubeReserved`; one that declares anything is taken at its word.
- The `HCloudNodeClass` CRD carries `spec.kubelet`. Helm does not upgrade CRDs, so apply them before `helm upgrade` from upstream 3.0.1, as the README describes.

### Changed

- **Breaking:** the image is published to `ghcr.io/mikluko/karpenter-provider-hetzner` and the chart to `oci://ghcr.io/mikluko/charts/karpenter-provider-hetzner`, both versioned from 0.1.0.
- **Breaking:** the Go module path is `github.com/mikluko/karpenter-provider-hetzner`.

### Removed

- **Breaking:** the `talos` and `ubuntu` image families; `debian` is the only one.
- The chart's Artifact Hub annotations.

[Unreleased]: https://github.com/mikluko/karpenter-provider-hetzner/compare/v0.4.0...HEAD
[0.4.0]: https://github.com/mikluko/karpenter-provider-hetzner/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/mikluko/karpenter-provider-hetzner/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/mikluko/karpenter-provider-hetzner/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/mikluko/karpenter-provider-hetzner/releases/tag/v0.1.0
