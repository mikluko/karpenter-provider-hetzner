// Copyright The karpenter-provider-hetzner Authors.
// SPDX-License-Identifier: Apache-2.0

package bootstrap

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/textproto"
	"regexp"
	"strings"
)

// JoinEnvPath is the file every join, generated or replaced, reads its inputs from.
const JoinEnvPath = "/etc/karpenter-hetzner/join.env"

// JoinEnv is what the provider knows about a node before it boots.
type JoinEnv struct {
	// JoinToken is the credential the node joins with.
	JoinToken string
	// NodeName is the NodeClaim name, which is also the server's hostname.
	NodeName string
	// RegisterTaints are the taints the kubelet registers with, in
	// --register-with-taints syntax.
	RegisterTaints []string
}

// Parts are the operator's cloud-init documents around the join. An empty
// part is left out; an empty Join is replaced by the generated k0s join.
type Parts struct {
	BeforeJoin string
	Join       string
	AfterJoin  string
}

// Script part filenames. cloud-init runs every shell-script part and the
// script it writes for cloud-config runcmd from one directory, in lexical
// filename order, and names the runcmd script "runcmd". These names put
// before-join ahead of runcmd and the join and after-join behind it.
const (
	filenameJoinEnv    = "join-env"
	filenameBeforeJoin = "10-before-join"
	filenameJoin       = "z10-join"
	filenameAfterJoin  = "z20-after-join"
)

var k0sVersionRE = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+\+k0s\.[0-9]+$`)

// UserData renders the cloud-init MIME multipart a server boots with: a
// boothook writing env to JoinEnvPath, then before-join, the join, and
// after-join. k0sVersion is used only when parts.Join is empty.
func UserData(env JoinEnv, parts Parts, k0sVersion string) (string, error) {
	join := parts.Join
	if join == "" {
		var err error
		if join, err = K0sJoin(k0sVersion); err != nil {
			return "", err
		}
	}

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for _, p := range []struct{ filename, contentType, content string }{
		{filenameJoinEnv, "text/cloud-boothook", joinEnvBoothook(env)},
		{filenameBeforeJoin, "text/plain", parts.BeforeJoin},
		{filenameJoin, "text/plain", join},
		{filenameAfterJoin, "text/plain", parts.AfterJoin},
	} {
		if p.content == "" {
			continue
		}
		pw, err := w.CreatePart(textproto.MIMEHeader{
			"Content-Type":        {p.contentType + `; charset="utf-8"`},
			"Content-Disposition": {fmt.Sprintf("attachment; filename=%q", p.filename)},
		})
		if err != nil {
			return "", err
		}
		if _, err := pw.Write([]byte(p.content)); err != nil {
			return "", err
		}
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	return fmt.Sprintf("Content-Type: multipart/mixed; boundary=%q\nMIME-Version: 1.0\n\n%s", w.Boundary(), body.String()), nil
}

// joinEnvBoothook writes env to JoinEnvPath on first boot and leaves the file
// alone after, so additions a before-join part makes survive a reboot.
func joinEnvBoothook(env JoinEnv) string {
	var b strings.Builder
	b.WriteString("#cloud-boothook\n#!/bin/sh\nset -eu\n")
	fmt.Fprintf(&b, "[ -e %[1]s ] && exit 0\numask 077\nmkdir -p %[2]s\ncat > %[1]s <<'EOF'\n", JoinEnvPath, JoinEnvPath[:strings.LastIndex(JoinEnvPath, "/")])
	fmt.Fprintf(&b, "JOIN_TOKEN=%s\n", shellQuote(env.JoinToken))
	fmt.Fprintf(&b, "NODE_NAME=%s\n", shellQuote(env.NodeName))
	fmt.Fprintf(&b, "REGISTER_TAINTS=%s\n", shellQuote(strings.Join(env.RegisterTaints, ",")))
	b.WriteString("EOF\n")
	return b.String()
}

// K0sJoin returns the generated join: it installs k0s k0sVersion and starts a
// worker named NODE_NAME with the provider ID read from the metadata service.
func K0sJoin(k0sVersion string) (string, error) {
	if !k0sVersionRE.MatchString(k0sVersion) {
		return "", fmt.Errorf("k0s version %q does not look like a k0s release tag such as v1.34.1+k0s.0", k0sVersion)
	}
	return fmt.Sprintf(`#!/bin/sh
set -eu
. %s
server_id=$(curl -fsS http://169.254.169.254/hetzner/v1/metadata/instance-id)
curl -fsSL https://get.k0s.sh | K0S_VERSION=%s sh
install -d -m 0700 /etc/k0s
(umask 077 && printf '%%s' "$JOIN_TOKEN" > /etc/k0s/join-token)
k0s install worker \
  --token-file /etc/k0s/join-token \
  --taints "$REGISTER_TAINTS" \
  --kubelet-extra-args "--provider-id=hcloud://$server_id --hostname-override=$NODE_NAME ${KUBELET_EXTRA_ARGS:-}"
k0s start
`, JoinEnvPath, k0sVersion), nil
}

// shellQuote quotes s as one POSIX shell word.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
