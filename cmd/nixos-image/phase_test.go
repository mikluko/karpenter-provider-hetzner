// Copyright The karpenter-provider-hetzner Authors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"testing"
)

// recordHandler keeps the messages logged through it.
type recordHandler struct{ msgs *[]string }

func (h recordHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h recordHandler) Handle(_ context.Context, r slog.Record) error {
	*h.msgs = append(*h.msgs, r.Message)
	return nil
}
func (h recordHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h recordHandler) WithGroup(string) slog.Handler      { return h }

func TestPhaseWriter(t *testing.T) {
	var msgs []string
	var raw bytes.Buffer
	w := &phaseWriter{w: &raw, log: slog.New(recordHandler{&msgs})}

	chunks := []string{
		"478850K .......... 99%  314M 0s\n",
		"### Uploading install SSH ke",
		"ys ###\n+ set -x\n",
		"[1/9 built] copying\r### Installing NixOS ###\r\n",
		"### not a banner\n",
		"  ### Done! ###",
	}
	for _, c := range chunks {
		if _, err := w.Write([]byte(c)); err != nil {
			t.Fatal(err)
		}
	}
	w.Flush()

	if want := []string{"Uploading install SSH keys", "Installing NixOS", "Done!"}; !slices.Equal(msgs, want) {
		t.Errorf("logged %q, want %q", msgs, want)
	}
	if got, want := raw.String(), "478850K .......... 99%  314M 0s\n### Uploading install SSH keys ###\n+ set -x\n[1/9 built] copying\r### Installing NixOS ###\r\n### not a banner\n  ### Done! ###"; got != want {
		t.Errorf("raw output = %q, want %q", got, want)
	}
}

func TestTailLines(t *testing.T) {
	for name, tc := range map[string]struct {
		in   string
		n    int
		want string
	}{
		"fewer lines than n": {"a\nb\n", 5, "a\nb\n"},
		"last n":             {"a\nb\nc\nd\n", 2, "c\nd\n"},
		"no final newline":   {"a\nb\nc", 2, "b\nc"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := tailLines([]byte(tc.in), tc.n); string(got) != tc.want {
				t.Errorf("tailLines = %q, want %q", got, tc.want)
			}
		})
	}
}
