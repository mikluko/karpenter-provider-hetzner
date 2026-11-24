// Copyright The karpenter-provider-hetzner Authors.
// SPDX-License-Identifier: Apache-2.0

// Command nixos-image builds a NixOS k0s worker snapshot in a Hetzner Cloud
// project: it installs a worker configuration of the image flake onto a
// throwaway server with nixos-anywhere, snapshots the server and deletes it.
// It reads the API token from HCLOUD_TOKEN, and needs nix, ssh and ssh-keygen
// on PATH.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "nixos-image:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := buildConfig{Labels: map[string]string{}}
	arch := flag.String("arch", "", "architecture to build for: x86 or arm")
	flag.StringVar(&cfg.K0sMinor, "k0s", "", "k0s minor version, such as 1.36")
	flag.StringVar(&cfg.Location, "location", "nbg1", "location of the builder server")
	flag.StringVar(&cfg.ServerType, "server-type", "", "builder server type; by default the cheapest of the smallest disk")
	flag.StringVar(&cfg.Flake, "flake", "./image", "the image flake")
	flag.BoolVar(&cfg.Keep, "keep", false, "keep the builder server when the build fails")
	flag.Func("label", "snapshot label as key=value, repeatable; overrides a default", func(s string) error {
		k, v, ok := strings.Cut(s, "=")
		if !ok || k == "" {
			return errors.New("want key=value")
		}
		cfg.Labels[k] = v
		return nil
	})
	flag.Parse()

	cfg.Arch = hcloud.Architecture(*arch)
	if cfg.Arch != hcloud.ArchitectureX86 && cfg.Arch != hcloud.ArchitectureARM {
		return fmt.Errorf("-arch must be x86 or arm, got %q", *arch)
	}
	if cfg.K0sMinor == "" {
		return errors.New("-k0s is required")
	}
	token := os.Getenv("HCLOUD_TOKEN")
	if token == "" {
		return errors.New("HCLOUD_TOKEN is not set")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	b := &builder{
		cloud: hcloudAPI{hcloud.NewClient(hcloud.WithToken(token), hcloud.WithApplication("nixos-image", version()))},
		tools: localTools{out: os.Stderr},
		log:   log,
		now:   time.Now,
	}
	img, err := b.build(ctx, cfg)
	if err != nil {
		return err
	}
	log.Info("snapshot ready", "id", img.ID)
	fmt.Println(img.ID)
	return nil
}

func version() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		return bi.Main.Version
	}
	return "unknown"
}
