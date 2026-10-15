// Copyright The karpenter-provider-hetzner Authors.
// SPDX-License-Identifier: Apache-2.0

package repo_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// licenseHeader opens every Go source file. hack/boilerplate.go.txt carries the
// same text for controller-gen.
const licenseHeader = "// Copyright The karpenter-provider-hetzner Authors.\n" +
	"// SPDX-License-Identifier: Apache-2.0\n\n"

func TestGoFilesCarryLicenseHeader(t *testing.T) {
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != "." && (strings.HasPrefix(d.Name(), ".") || d.Name() == "bin") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(skipBuildConstraint(string(src)), licenseHeader) {
			t.Errorf("%s: missing license header", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// skipBuildConstraint drops a leading //go:build line and the blank line after
// it. controller-gen writes the constraint above the header.
func skipBuildConstraint(src string) string {
	if !strings.HasPrefix(src, "//go:build ") {
		return src
	}
	_, rest, _ := strings.Cut(src, "\n")
	return strings.TrimPrefix(rest, "\n")
}

func TestBoilerplateMatchesLicenseHeader(t *testing.T) {
	src, err := os.ReadFile("hack/boilerplate.go.txt")
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.TrimSuffix(licenseHeader, "\n"); string(src) != want {
		t.Errorf("hack/boilerplate.go.txt = %q, want %q", src, want)
	}
}
