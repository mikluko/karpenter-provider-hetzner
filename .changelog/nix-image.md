### Changed

- The image is built by the Nix flake, `nix build .#image-amd64` or `.#image-arm64`. Its entrypoint is the controller under `/nix/store`, so a `command` value naming `/karpenter-provider-hetzner` no longer starts it.

### Removed

- Images are no longer tagged `X.Y`: each release is published as `X.Y.Z` only.
