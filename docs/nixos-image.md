# Build a NixOS Worker Image

This guide shows you how to build a NixOS k0s worker snapshot in your Hetzner
Cloud project, and how to point a node class at it. Hetzner sells no NixOS
image, so a `nixos` node class boots a snapshot of your own.

`task image` builds one snapshot per architecture for a k0s minor version, the
architectures concurrently. For each, it creates a throwaway server, installs a
worker configuration of the [`image`](../image) flake onto it with
[nixos-anywhere](https://github.com/nix-community/nixos-anywhere), snapshots
the server, and deletes it. The build runs on the server, so your machine needs
no Linux builder. A build takes about six minutes.

## Before you begin

- [Nix](https://nixos.org/download/) with flakes enabled, `ssh`, `ssh-keygen`,
  Go, and [Task](https://taskfile.dev/).
- A read-write API token for the Hetzner Cloud project Karpenter creates nodes
  in, in `HCLOUD_TOKEN`.
- The k0s minor version of your control plane, such as `1.36`. On a
  control-plane node, `sudo k0s version` prints the full release tag.

## Build the snapshot

1. From the repository root, build for x86 and Arm:

   ```bash
   task image K0S=1.36
   ```

   To build one architecture, add `ARCH=x86` or `ARCH=arm`.

   The run ends with one line per architecture, such as `arm 460469349`, the
   snapshot ID. The description reads similar to
   `nixos-26.05 k0s-v1.36.6+k0s.0`.

1. Optional: pass options after `--`. To build in another location, or to add a
   label of your own:

   ```bash
   task image K0S=1.36 -- -location fsn1 -label pool=general
   ```

   `go run ./cmd/nixos-image -h` lists every option.

The terminal shows each build's progress, tagged with its architecture. The
output of nix, nixos-anywhere and ssh goes to a log file per build, whose path
the build prints first; `-v` streams it to the terminal too.

The flake sees only the files git tracks, so `git add` a new file under
`image/` before a build. A build from uncommitted changes gives the snapshot a
`rev` label ending in `-dirty`.

## Select the snapshot in a node class

Every snapshot carries `role=k0s-worker` and `k0s-minor` among its labels; the
set is `snapshotLabels` in [`cmd/nixos-image/build.go`](../cmd/nixos-image/build.go).
Name the labels in `imageSelector.selector`, which a `nixos` node class
requires:

```yaml
imageSelector:
  family: nixos
  selector:
    role: k0s-worker
    k0s-minor: "1.36"
bootstrap: {}
```

A `nixos` node class takes no `bootstrap.k0sVersion`: the generated join starts
the k0s worker the image carries.

The provider resolves the newest matching snapshot per architecture. To pin a
k0s patch release, set `imageSelector.version` to a substring of the
description, such as `k0s-v1.36.6`.

A snapshot boots only on server types whose disk is at least as large as its
builder's. The builder is the cheapest type of the smallest disk with at least
2 GB of memory, so every server type in the builder's location with that much
memory can boot it. `-server-type` picks the builder yourself.

## If a build fails

A failed build prints the last lines of its log, and the log's path. Its server
and SSH key are deleted when it fails or is interrupted. To debug an install,
keep the server of a failed build with `-keep`, and delete it yourself
afterwards:

```bash
task image ARCH=x86 K0S=1.36 -- -keep
```

A run that was killed outright leaves its server and SSH key behind. Both carry
the label `nixos-image-build`; find them in the Cloud Console or with
`hcloud server list --selector nixos-image-build`.
