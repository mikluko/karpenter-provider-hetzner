// Copyright The karpenter-provider-hetzner Authors.
// SPDX-License-Identifier: Apache-2.0

// Command nixos-image builds NixOS k0s worker snapshots in a Hetzner Cloud
// project, one per architecture, concurrently: each build installs a worker
// configuration of the image flake onto a throwaway server with
// nixos-anywhere, snapshots the server and deletes it. It reads the API token
// from HCLOUD_TOKEN, and needs nix, ssh and ssh-keygen on PATH.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// failureTail is how many lines of a failed build's log are printed.
const failureTail = 40

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "nixos-image:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := buildConfig{Labels: map[string]string{}}
	archList := flag.String("arch", "x86,arm", "architectures to build, comma-separated: x86, arm")
	flag.StringVar(&cfg.K0sMinor, "k0s", "", "k0s minor version, such as 1.36")
	flag.StringVar(&cfg.Location, "location", "nbg1", "location of the builder servers")
	flag.StringVar(&cfg.ServerType, "server-type", "", "builder server type, for a single -arch; by default the cheapest of the smallest disk with 2 GB of memory")
	flag.StringVar(&cfg.Flake, "flake", "./image", "the image flake")
	flag.BoolVar(&cfg.Keep, "keep", false, "keep the builder server when a build fails")
	logDir := flag.String("log-dir", os.TempDir(), "directory for each build's log of nix, nixos-anywhere and ssh output")
	verbose := flag.Bool("v", false, "also stream that output to stderr")
	flag.Func("label", "snapshot label as key=value, repeatable; overrides a default", func(s string) error {
		k, v, ok := strings.Cut(s, "=")
		if !ok || k == "" {
			return errors.New("want key=value")
		}
		cfg.Labels[k] = v
		return nil
	})
	flag.Parse()

	arches, err := parseArches(*archList)
	if err != nil {
		return err
	}
	if cfg.ServerType != "" && len(arches) > 1 {
		return errors.New("-server-type needs a single -arch")
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

	r := runner{
		cloud:   hcloudAPI{hcloud.NewClient(hcloud.WithToken(token), hcloud.WithApplication("nixos-image", version()))},
		log:     slog.New(slog.NewTextHandler(os.Stderr, nil)),
		logDir:  *logDir,
		verbose: *verbose,
	}
	results := make([]result, len(arches))
	var wg sync.WaitGroup
	for i, arch := range arches {
		c := cfg
		c.Arch = arch
		wg.Go(func() { results[i] = r.build(ctx, c) })
	}
	wg.Wait()

	var errs []error
	for _, res := range results {
		if res.err != nil {
			reportFailure(os.Stderr, res)
			errs = append(errs, fmt.Errorf("%s: %w", res.arch, res.err))
			continue
		}
		fmt.Printf("%s %d\n", res.arch, res.image.ID)
	}
	return errors.Join(errs...)
}

// parseArches returns the architectures in a comma-separated list, once each.
func parseArches(list string) ([]hcloud.Architecture, error) {
	var arches []hcloud.Architecture
	for s := range strings.SplitSeq(list, ",") {
		a := hcloud.Architecture(strings.TrimSpace(s))
		if a != hcloud.ArchitectureX86 && a != hcloud.ArchitectureARM {
			return nil, fmt.Errorf("-arch takes x86 and arm, got %q", s)
		}
		if !slices.Contains(arches, a) {
			arches = append(arches, a)
		}
	}
	return arches, nil
}

// runner builds one architecture's snapshot, logging the output of the tools
// it runs to a file of its own.
type runner struct {
	cloud   cloud
	log     *slog.Logger
	logDir  string
	verbose bool
}

type result struct {
	arch    hcloud.Architecture
	image   *hcloud.Image
	logPath string
	err     error
}

func (r runner) build(ctx context.Context, cfg buildConfig) (res result) {
	res.arch = cfg.Arch
	log := r.log.With("arch", cfg.Arch)
	path := filepath.Join(r.logDir, fmt.Sprintf("nixos-image-%s-%d.log", cfg.Arch, time.Now().Unix()))
	f, err := os.Create(path)
	if err != nil {
		res.err = err
		return res
	}
	res.logPath = path
	defer func() { res.err = errors.Join(res.err, f.Close()) }()
	log.Info("logging tool output", "path", path)

	var out io.Writer = f
	if r.verbose {
		out = io.MultiWriter(f, os.Stderr)
	}
	pw := &phaseWriter{w: out, log: log}
	defer pw.Flush()

	b := &builder{cloud: r.cloud, tools: localTools{out: pw}, log: log, now: time.Now}
	res.image, res.err = b.build(ctx, cfg)
	return res
}

// reportFailure prints the end of a failed build's log.
func reportFailure(w io.Writer, res result) {
	if res.logPath == "" {
		return
	}
	b, err := os.ReadFile(res.logPath)
	if err != nil || len(b) == 0 {
		return
	}
	tail := tailLines(b, failureTail)
	if tail[len(tail)-1] != '\n' {
		tail = append(tail, '\n')
	}
	_, _ = fmt.Fprintf(w, "--- %s: last %d lines of %s\n%s", res.arch, failureTail, res.logPath, tail)
}

func version() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		return bi.Main.Version
	}
	return "unknown"
}
