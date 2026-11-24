// Copyright The karpenter-provider-hetzner Authors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"strings"
	"time"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// buildLabel marks the throwaway server and SSH key of a build, so that a run
// killed before its cleanup leaves resources a label selector finds.
const buildLabel = "karpenter.hetzner.cloud/nixos-image-build"

// builderImage is the system image a builder boots before nixos-anywhere
// kexecs it into the NixOS installer.
const builderImage = "debian-13"

// cleanupTimeout bounds the deletes that run after the build, including after
// the build's own context was cancelled.
const cleanupTimeout = 2 * time.Minute

// cloud is the Hetzner Cloud API a build needs.
type cloud interface {
	ServerTypes(ctx context.Context) ([]*hcloud.ServerType, error)
	CreateSSHKey(ctx context.Context, name, publicKey string, labels map[string]string) (*hcloud.SSHKey, error)
	DeleteSSHKey(ctx context.Context, key *hcloud.SSHKey) error
	// CreateServer returns once the server is running.
	CreateServer(ctx context.Context, opts hcloud.ServerCreateOpts) (*hcloud.Server, error)
	PowerOff(ctx context.Context, server *hcloud.Server) error
	// Snapshot returns once the snapshot is available.
	Snapshot(ctx context.Context, server *hcloud.Server, description string, labels map[string]string) (*hcloud.Image, error)
	DeleteServer(ctx context.Context, server *hcloud.Server) error
}

// tools runs the local programs a build shells out to: nix, ssh, ssh-keygen.
type tools interface {
	Describe(ctx context.Context, flake, config string) (imageInfo, error)
	// KeyPair writes a new key pair into dir and returns the private key's
	// path and the public key in authorized_keys form.
	KeyPair(ctx context.Context, dir string) (privateKey, publicKey string, err error)
	WaitSSH(ctx context.Context, addr, privateKey string) error
	Install(ctx context.Context, flake, config, addr, privateKey string) error
	// Unmount unmounts the installed system so the disk is safe to power off.
	Unmount(ctx context.Context, addr, privateKey string) error
}

// imageInfo is what a worker configuration reports about itself.
type imageInfo struct {
	NixOS string // NixOS release, such as 26.05
	K0s   string // k0s version without the leading v, such as 1.36.6+k0s.0
	Rev   string // flake revision, suffixed -dirty for an uncommitted tree
}

type buildConfig struct {
	Arch       hcloud.Architecture
	K0sMinor   string // such as 1.36
	Location   string
	ServerType string // empty picks one with builderTypes
	Flake      string
	Labels     map[string]string // added to, and overriding, the snapshot's defaults
	Keep       bool              // keep the server of a failed build
}

// workerConfig names the flake's nixosConfiguration for arch and minor.
func workerConfig(arch hcloud.Architecture, minor string) string {
	return "k0s_" + strings.ReplaceAll(minor, ".", "_") + "-" + string(arch)
}

// snapshotDescription is matched by imageSelector.version, as a substring.
func snapshotDescription(info imageInfo) string {
	return fmt.Sprintf("nixos-%s k0s-v%s", info.NixOS, info.K0s)
}

func snapshotLabels(cfg buildConfig, info imageInfo) map[string]string {
	labels := map[string]string{
		"role":      "k0s-worker",
		"nixos":     info.NixOS,
		"k0s-minor": cfg.K0sMinor,
		"rev":       info.Rev,
	}
	maps.Copy(labels, cfg.Labels)
	return labels
}

type builder struct {
	cloud cloud
	tools tools
	log   *slog.Logger
	now   func() time.Time
}

// build installs the worker configuration for cfg on a throwaway server and
// returns the snapshot taken of it. The server and its SSH key are deleted
// whatever the outcome, except the server of a failed build when cfg.Keep is
// set.
func (b *builder) build(ctx context.Context, cfg buildConfig) (_ *hcloud.Image, err error) {
	config := workerConfig(cfg.Arch, cfg.K0sMinor)
	info, err := b.tools.Describe(ctx, cfg.Flake, config)
	if err != nil {
		return nil, fmt.Errorf("evaluating %s: %w", config, err)
	}
	b.log.Info("building", "config", config, "nixos", info.NixOS, "k0s", info.K0s, "rev", info.Rev)

	types, err := b.serverTypes(ctx, cfg)
	if err != nil {
		return nil, err
	}

	dir, err := os.MkdirTemp("", "nixos-image-")
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(dir)) }()

	privateKey, publicKey, err := b.tools.KeyPair(ctx, dir)
	if err != nil {
		return nil, fmt.Errorf("generating an SSH key: %w", err)
	}

	name := fmt.Sprintf("nixos-image-%s-%d", cfg.Arch, b.now().Unix())
	temp := map[string]string{buildLabel: "true"}

	key, err := b.cloud.CreateSSHKey(ctx, name, publicKey, temp)
	if err != nil {
		return nil, fmt.Errorf("creating SSH key: %w", err)
	}
	defer b.cleanup(ctx, &err, "SSH key", func(ctx context.Context) error { return b.cloud.DeleteSSHKey(ctx, key) })

	server, err := b.createServer(ctx, cfg, types, name, key, temp)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil && cfg.Keep {
			b.log.Warn("keeping the server of a failed build", "server", server.Name, "id", server.ID)
			return
		}
		b.cleanup(ctx, &err, "server", func(ctx context.Context) error { return b.cloud.DeleteServer(ctx, server) })
	}()

	addr := server.PublicNet.IPv4.IP.String()
	b.log.Info("server running", "server", server.Name, "type", server.ServerType.Name, "addr", addr)

	if err := b.tools.WaitSSH(ctx, addr, privateKey); err != nil {
		return nil, fmt.Errorf("waiting for SSH on %s: %w", addr, err)
	}
	if err := b.tools.Install(ctx, cfg.Flake, config, addr, privateKey); err != nil {
		return nil, fmt.Errorf("installing %s: %w", config, err)
	}
	if err := b.tools.Unmount(ctx, addr, privateKey); err != nil {
		return nil, fmt.Errorf("unmounting the installed system: %w", err)
	}
	if err := b.cloud.PowerOff(ctx, server); err != nil {
		return nil, fmt.Errorf("powering off: %w", err)
	}

	b.log.Info("taking snapshot", "server", server.Name)
	img, err := b.cloud.Snapshot(ctx, server, snapshotDescription(info), snapshotLabels(cfg, info))
	if err != nil {
		return nil, fmt.Errorf("taking snapshot: %w", err)
	}
	return img, nil
}

// serverTypes returns the server types to try, in order.
func (b *builder) serverTypes(ctx context.Context, cfg buildConfig) ([]*hcloud.ServerType, error) {
	if cfg.ServerType != "" {
		return []*hcloud.ServerType{{Name: cfg.ServerType}}, nil
	}
	all, err := b.cloud.ServerTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing server types: %w", err)
	}
	types := builderTypes(all, cfg.Arch, cfg.Location, b.now())
	if len(types) == 0 {
		return nil, fmt.Errorf("no %s server type on sale in %s", cfg.Arch, cfg.Location)
	}
	return types, nil
}

// createServer tries each server type in turn, moving on while hcloud reports
// one out of stock.
func (b *builder) createServer(ctx context.Context, cfg buildConfig, types []*hcloud.ServerType, name string, key *hcloud.SSHKey, labels map[string]string) (*hcloud.Server, error) {
	var errs []error
	for _, st := range types {
		server, err := b.cloud.CreateServer(ctx, hcloud.ServerCreateOpts{
			Name:       name,
			ServerType: st,
			Image:      &hcloud.Image{Name: builderImage},
			Location:   &hcloud.Location{Name: cfg.Location},
			SSHKeys:    []*hcloud.SSHKey{key},
			Labels:     labels,
			PublicNet:  &hcloud.ServerCreatePublicNet{EnableIPv4: true, EnableIPv6: true},
		})
		if err == nil {
			return server, nil
		}
		if !hcloud.IsError(err, hcloud.ErrorCodeResourceUnavailable) {
			return nil, fmt.Errorf("creating server %s: %w", st.Name, err)
		}
		b.log.Info("server type unavailable, trying the next", "type", st.Name)
		errs = append(errs, err)
	}
	return nil, fmt.Errorf("no server type available in %s: %w", cfg.Location, errors.Join(errs...))
}

// cleanup runs del on a context that outlives a cancelled build, and joins
// its error into *err.
func (b *builder) cleanup(ctx context.Context, err *error, what string, del func(context.Context) error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	if derr := del(ctx); derr != nil {
		*err = errors.Join(*err, fmt.Errorf("deleting %s: %w", what, derr))
		return
	}
	b.log.Info("deleted", "resource", what)
}
