# karpenter-provider-hetzner

[![CI](https://github.com/mikluko/karpenter-provider-hetzner/actions/workflows/ci.yaml/badge.svg)](https://github.com/mikluko/karpenter-provider-hetzner/actions/workflows/ci.yaml)
[![Go Report Card](https://goreportcard.com/badge/github.com/mikluko/karpenter-provider-hetzner)](https://goreportcard.com/report/github.com/mikluko/karpenter-provider-hetzner)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

A [Karpenter](https://karpenter.sh) cloud provider for
[Hetzner Cloud](https://www.hetzner.com/cloud). It creates Hetzner Cloud servers
as Kubernetes nodes for pending pods, choosing the cheapest server type that fits
from Hetzner's live pricing, and removes them when they are no longer needed.

## Status

The project is pre-1.0. It continues upstream karpenter-provider-hetzner 3.0.1;
see the [upstream changelog](CHANGELOG.upstream.md) for its history. Release
images are multi-arch, signed with cosign, and carry SLSA provenance and an
SBOM. Pin a released version in production.

## Features

- All Hetzner Cloud server types: CX, CPX, CAX (Arm), and CCX.
- Cheapest-first selection, priced from Hetzner's net hourly price.
- Capacity awareness: an offering Hetzner reports as unavailable in a location
  is skipped for a few minutes, and Karpenter falls back to another.
- Drift detection on image, network, firewall, and server type, which replaces
  the node.
- Debian images, Hetzner's own or your labelled snapshots, resolved per architecture.
- Placement groups that spread nodes across physical hosts.
- Optional public IPv4 and IPv6, for clusters on a private network.
- Several clusters per Hetzner project: every server carries its cluster's name,
  and a controller touches only its own.

## How it works

A `NodePool` references an `HCloudNodeClass`. When pods are unschedulable,
Karpenter asks this provider for the instance types it can offer, picks the
cheapest compatible offering, and the provider creates the server. The server
boots with the `userData` from the node class, which joins it to the cluster.

## Install

You need:

- a Kubernetes cluster on Hetzner Cloud whose nodes have provider IDs of the form
  `hcloud://<server-id>`, set by the
  [hcloud Cloud Controller Manager](https://github.com/hetznercloud/hcloud-cloud-controller-manager)
  or by the node's bootstrap, as
  [About node bootstrap](docs/about-node-bootstrap.md#what-karpenter-expects-of-a-new-node)
  describes;
- a Hetzner Cloud API token with read and write access.

To install:

1. Store the token in a Secret:

   ```bash
   kubectl create secret generic hcloud-token \
     --namespace kube-system \
     --from-literal=token=<hcloud-token>
   ```

1. Install the chart, with `<cluster-name>` naming this cluster:

   ```bash
   helm install karpenter-provider-hetzner \
     oci://ghcr.io/mikluko/charts/karpenter-provider-hetzner \
     --namespace kube-system \
     --set clusterName=<cluster-name> \
     --set auth.secretRef.name=hcloud-token
   ```

`clusterName` is required. The controller manages only servers labelled with it,
and does not start without it.

The chart installs three CRDs from its `crds/` directory: `HCloudNodeClass`, and
the Karpenter `NodePool` and `NodeClaim`.

## Upgrade

Helm installs the CRDs in `crds/` and never updates them. When the release notes
say the CRDs changed, apply them before upgrading the chart:

1. Apply the CRDs of the release, with `<version>` naming it, for example `v0.1.0`:

   ```bash
   BASE=https://raw.githubusercontent.com/mikluko/karpenter-provider-hetzner/<version>/charts/karpenter-provider-hetzner/crds
   kubectl apply --server-side --force-conflicts \
     -f $BASE/karpenter.hetzner.cloud_hcloudnodeclasses.yaml \
     -f $BASE/karpenter.sh_nodeclaims.yaml \
     -f $BASE/karpenter.sh_nodepools.yaml
   ```

   `--force-conflicts` takes the fields over from Helm, which created the CRDs.

1. Upgrade the chart, with `<chart-version>` naming the same release without the
   `v`, for example `0.1.0`:

   ```bash
   helm upgrade karpenter-provider-hetzner \
     oci://ghcr.io/mikluko/charts/karpenter-provider-hetzner \
     --namespace kube-system --version <chart-version> --reuse-values
   ```

If a GitOps tool manages your CRDs, update them there instead; it reverts a
manual apply.

## Provision nodes

Create an `HCloudNodeClass` that says how servers are built, and a `NodePool`
that says what Karpenter may create:

```yaml
apiVersion: karpenter.hetzner.cloud/v1
kind: HCloudNodeClass
metadata:
  name: default
spec:
  locations: [nbg1, fsn1]
  networkID: 123456
  imageSelector:
    family: debian
    version: "13"
  enablePublicIPv4: false
  userDataSecretRef:
    namespace: kube-system
    name: debian-worker-userdata
    key: cloud-init.yaml
---
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: default
spec:
  template:
    spec:
      nodeClassRef:
        group: karpenter.hetzner.cloud
        kind: HCloudNodeClass
        name: default
      requirements:
        - key: karpenter.sh/capacity-type
          operator: In
          values: [on-demand]
        - key: kubernetes.io/arch
          operator: In
          values: [amd64]
        - key: karpenter.hetzner.cloud/server-family
          operator: In
          values: [cx, cpx]
  limits:
    cpu: "100"
  disruption:
    consolidationPolicy: WhenEmptyOrUnderutilized
```

The `userData` is a cloud-init document that joins the server to your cluster.
[Join Debian nodes with kubeadm](docs/debian-bootstrap.md) walks through one, and
[About node bootstrap](docs/about-node-bootstrap.md) covers what any bootstrap
has to provide.

The [`examples/`](examples/) directory holds the node class and node pool from
that guide, and a pair of node pools for amd64 and arm64.

Every field, status condition, node label, and controller setting is listed in
the [reference](docs/reference.md).

## Scheduling and cost

- Hetzner sells no spot capacity, so pin `karpenter.sh/capacity-type` to
  `on-demand` in every NodePool. Without it, Karpenter requests `spot` for every
  consolidation replacement, no offering matches, and the replacement never
  launches. New nodes and other disruptions still work, which hides the fault.
- Selection is cheapest-first. A NodePool that allows several architectures gets
  whichever is cheapest for the requested shape, which may be Arm (CAX). Allow
  `arm64` only once your images are multi-arch.
- Prices are Hetzner's net figures, before VAT, as on the invoice.
- Hetzner bills a primary IPv4 separately. On a private-network cluster, set
  `enablePublicIPv4: false` to drop it.

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md) and the
[Code of Conduct](CODE_OF_CONDUCT.md).

## Security

To report a vulnerability, follow [SECURITY.md](SECURITY.md). Do not open a public
issue.

## License

[Apache 2.0](LICENSE).
