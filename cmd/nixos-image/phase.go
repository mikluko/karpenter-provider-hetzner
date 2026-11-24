// Copyright The karpenter-provider-hetzner Authors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"io"
	"log/slog"
	"strings"
	"sync"
)

// maxLine caps the bytes kept of a line while looking for a phase banner; a
// banner is short, and progress output can run long without a newline.
const maxLine = 256

// phaseWriter writes everything to w, and logs to log each nixos-anywhere
// phase banner it sees, a line such as "### Installing NixOS ###". It is safe
// for concurrent use.
type phaseWriter struct {
	w   io.Writer
	log *slog.Logger

	mu   sync.Mutex
	line []byte
}

func (p *phaseWriter) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	n, err := p.w.Write(b)
	for _, c := range b[:n] {
		switch c {
		case '\n', '\r':
			p.endLine()
		default:
			if len(p.line) < maxLine {
				p.line = append(p.line, c)
			}
		}
	}
	return n, err
}

// Flush logs a banner left on a line with no newline after it.
func (p *phaseWriter) Flush() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.endLine()
}

func (p *phaseWriter) endLine() {
	s := strings.TrimSpace(string(p.line))
	p.line = p.line[:0]
	if phase, ok := strings.CutPrefix(s, "### "); ok {
		if phase, ok := strings.CutSuffix(phase, " ###"); ok {
			p.log.Info(phase)
		}
	}
}

// tailLines returns the last n lines of b.
func tailLines(b []byte, n int) []byte {
	end := len(b)
	if end > 0 && b[end-1] == '\n' {
		end--
	}
	for i := end - 1; i >= 0; i-- {
		if b[i] == '\n' {
			n--
			if n == 0 {
				return b[i+1:]
			}
		}
	}
	return b
}
