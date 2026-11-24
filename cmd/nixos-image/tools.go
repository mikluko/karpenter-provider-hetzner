// Copyright The karpenter-provider-hetzner Authors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// sshOptions trust whatever host key the builder presents: the host is a
// minute old, and its key changes when nixos-anywhere kexecs it.
var sshOptions = []string{
	"-o", "StrictHostKeyChecking=no",
	"-o", "UserKnownHostsFile=/dev/null",
	"-o", "LogLevel=ERROR",
	"-o", "BatchMode=yes",
	"-o", "ConnectTimeout=5",
}

// sshWait bounds how long a fresh builder may take to accept the build's key.
const sshWait = 5 * time.Minute

// localTools runs nix, ssh and ssh-keygen from PATH, streaming their output to
// out.
type localTools struct{ out io.Writer }

func (t localTools) Describe(ctx context.Context, flake, config string) (imageInfo, error) {
	attr := flake + "#nixosConfigurations." + config + ".config."
	nixos, err := t.output(ctx, "nix", "eval", "--raw", attr+"system.nixos.release")
	if err != nil {
		return imageInfo{}, err
	}
	k0s, err := t.output(ctx, "nix", "eval", "--raw", attr+"services.k0s.package.version")
	if err != nil {
		return imageInfo{}, err
	}
	meta, err := t.output(ctx, "nix", "flake", "metadata", "--json", flake)
	if err != nil {
		return imageInfo{}, err
	}
	rev, err := flakeRevision([]byte(meta))
	if err != nil {
		return imageInfo{}, err
	}
	return imageInfo{NixOS: nixos, K0s: k0s, Rev: rev}, nil
}

// flakeRevision returns the git revision in nix flake metadata output, which
// names it dirtyRevision, suffixed -dirty, for an uncommitted tree.
func flakeRevision(meta []byte) (string, error) {
	var m struct {
		Revision      string `json:"revision"`
		DirtyRevision string `json:"dirtyRevision"`
	}
	if err := json.Unmarshal(meta, &m); err != nil {
		return "", fmt.Errorf("reading flake metadata: %w", err)
	}
	if m.Revision != "" {
		return m.Revision, nil
	}
	if m.DirtyRevision != "" {
		return m.DirtyRevision, nil
	}
	return "", fmt.Errorf("flake metadata names no git revision")
}

func (t localTools) KeyPair(ctx context.Context, dir string) (string, string, error) {
	path := filepath.Join(dir, "id_ed25519")
	if _, err := t.output(ctx, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "nixos-image", "-f", path); err != nil {
		return "", "", err
	}
	pub, err := os.ReadFile(path + ".pub")
	if err != nil {
		return "", "", err
	}
	return path, strings.TrimSpace(string(pub)), nil
}

func (t localTools) WaitSSH(ctx context.Context, addr, privateKey string) error {
	ctx, cancel := context.WithTimeout(ctx, sshWait)
	defer cancel()
	for {
		out, err := exec.CommandContext(ctx, "ssh", t.sshArgs(addr, privateKey, "true")...).CombinedOutput()
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: last attempt: %w: %s", ctx.Err(), err, bytes.TrimSpace(out))
		case <-time.After(5 * time.Second):
		}
	}
}

func (t localTools) Install(ctx context.Context, flake, config, kexecURL, addr, privateKey string) error {
	args := []string{
		"run", flake + "#nixos-anywhere", "--",
		"--flake", flake + "#" + config,
		"--kexec", kexecURL,
		"--build-on", "remote",
		"--phases", "kexec,disko,install",
		"-i", privateKey,
		"--target-host", "root@" + addr,
	}
	for i := 1; i < len(sshOptions); i += 2 {
		args = append(args, "--ssh-option", sshOptions[i])
	}
	return t.stream(ctx, "nix", args...)
}

func (t localTools) Unmount(ctx context.Context, addr, privateKey string) error {
	return t.stream(ctx, "ssh", t.sshArgs(addr, privateKey, "umount -R /mnt && sync")...)
}

func (t localTools) sshArgs(addr, privateKey, command string) []string {
	args := append([]string{"-i", privateKey}, sshOptions...)
	return append(args, "root@"+addr, command)
}

// output runs name and returns its trimmed stdout; stderr goes to t.out.
func (t localTools) output(ctx context.Context, name string, args ...string) (string, error) {
	var stdout bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = t.out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// stream runs name with stdout and stderr going to t.out.
func (t localTools) stream(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = t.out
	cmd.Stderr = t.out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}
