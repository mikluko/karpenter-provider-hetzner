# Contributing

Thanks for your interest in improving `karpenter-provider-hetzner`. This document covers how to set up, build, test, and submit changes.

## Development environment

- Go (see the `go` directive in [`go.mod`](go.mod)).
- [`task`](https://taskfile.dev), `git`, and `helm` (for chart changes).
- A Hetzner Cloud API token is only needed for end-to-end testing, not for unit tests.

## Common tasks

```bash
task test        # run unit + controller tests with the race detector
task lint        # run golangci-lint
task generate    # regenerate the CRD and deepcopy from the Go types
task build       # build the controller binary into ./bin
```

CI runs `task test`, `golangci-lint`, `task generate-verify` (fails if generated files are stale), and the controller tests. Run these locally before opening a pull request.

## Code generation

The `HCloudNodeClass` CRD and `zz_generated.deepcopy.go` are generated from the API types in `pkg/apis/v1` by `controller-gen`. If you change those types, run `task generate` and commit the regenerated files. CI will fail if they are out of date.

## Testing conventions

- Write tests first (TDD) for new behavior.
- Tests must assert real behavior, not mock internals. The provider packages use small in-package fakes that implement the narrow hcloud client interfaces; reuse those patterns.
- Keep `go test -race ./...` green.

## Commit and PR guidelines

- Use clear, conventional-ish commit subjects (`feat:`, `fix:`, `refactor:`, `docs:`, `test:`, `ci:`, `chore:`).
- Keep PRs focused. Separate refactors from behavior changes where practical.
- Fill out the pull request template, including how you verified the change.
- All status checks must pass before merge.

## Releases

`CHANGELOG.md` decides the version. To release, rename `## [Unreleased]` to
`## [X.Y.Z] - YYYY-MM-DD`, open a fresh `## [Unreleased]` above it, and merge
on the date you wrote.

On a push to `main`, `release-cut.yaml` runs CI, then tags the commit `vX.Y.Z`
when the newest entry names a version no tag carries. The tag starts
`release-roll.yaml`, which builds, signs, and publishes the image and the chart,
and creates the GitHub release last. A tag whose `release-roll.yaml` run failed
is not cut again: re-run that run. Run by hand, `release-roll.yaml` builds the
newest version without publishing it.

## Reporting bugs and requesting features

Use the issue templates. For security issues, follow [SECURITY.md](SECURITY.md) instead of opening a public issue.

## License

By contributing, you agree that your contributions are licensed under the [Apache 2.0 License](LICENSE).
