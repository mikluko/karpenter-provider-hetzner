// Copyright The karpenter-provider-hetzner Authors.
// SPDX-License-Identifier: Apache-2.0

package bootstrap

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	bootstrapapi "k8s.io/cluster-bootstrap/token/api"
	bootstraputil "k8s.io/cluster-bootstrap/token/util"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

func nodeClaim() *karpv1.NodeClaim {
	return &karpv1.NodeClaim{ObjectMeta: metav1.ObjectMeta{Name: "default-abcde", UID: types.UID("uid-1")}}
}

func TestNewTokenSecret(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	token, secret, err := NewTokenSecret(nodeClaim(), now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !bootstraputil.IsValidBootstrapToken(token) {
		t.Fatalf("token %q is not a valid bootstrap token", token)
	}
	id, sec, _ := strings.Cut(token, ".")
	if secret.Namespace != "kube-system" || secret.Name != "bootstrap-token-"+id {
		t.Errorf("secret is %s/%s, want kube-system/bootstrap-token-%s", secret.Namespace, secret.Name, id)
	}
	if secret.Type != bootstrapapi.SecretTypeBootstrapToken {
		t.Errorf("type = %q", secret.Type)
	}
	for k, want := range map[string]string{
		bootstrapapi.BootstrapTokenIDKey:               id,
		bootstrapapi.BootstrapTokenSecretKey:           sec,
		bootstrapapi.BootstrapTokenExpirationKey:       "2026-10-16T13:00:00Z",
		bootstrapapi.BootstrapTokenUsageAuthentication: "true",
	} {
		if got := secret.StringData[k]; got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if _, ok := secret.StringData[bootstrapapi.BootstrapTokenUsageSigningKey]; ok {
		t.Error("token must not be usable for signing")
	}
	refs := secret.OwnerReferences
	if len(refs) != 1 || refs[0].APIVersion != "karpenter.sh/v1" || refs[0].Kind != "NodeClaim" || refs[0].Name != "default-abcde" || refs[0].UID != "uid-1" {
		t.Errorf("owner references = %+v, want the NodeClaim", refs)
	}
	if secret.Labels[LabelNodeClaim] != "default-abcde" {
		t.Errorf("labels = %v", secret.Labels)
	}
}

func TestNewTokenSecret_NoUID(t *testing.T) {
	nc := nodeClaim()
	nc.UID = ""
	if _, _, err := NewTokenSecret(nc, time.Now(), time.Hour); err == nil {
		t.Fatal("expected an error for a NodeClaim without a UID")
	}
}

func TestK0sJoinToken(t *testing.T) {
	encoded, err := K0sJoinToken("https://10.0.0.2:6443", []byte("CA"), "abcdef.0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	kubeconfig, err := io.ReadAll(gz)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := clientcmd.Load(kubeconfig)
	if err != nil {
		t.Fatal(err)
	}
	ctx := cfg.Contexts[cfg.CurrentContext]
	if got := cfg.Clusters[ctx.Cluster]; got.Server != "https://10.0.0.2:6443" || string(got.CertificateAuthorityData) != "CA" {
		t.Errorf("cluster = %+v", got)
	}
	if got := cfg.AuthInfos[ctx.AuthInfo].Token; got != "abcdef.0123456789abcdef" {
		t.Errorf("token = %q", got)
	}
}

type part struct{ filename, contentType, content string }

func parseUserData(t *testing.T, userData string) []part {
	t.Helper()
	msg, err := mail.ReadMessage(strings.NewReader(userData))
	if err != nil {
		t.Fatal(err)
	}
	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/mixed" {
		t.Fatalf("content type %q: %v", msg.Header.Get("Content-Type"), err)
	}
	r := multipart.NewReader(msg.Body, params["boundary"])
	var parts []part
	for {
		p, err := r.NextPart()
		if err == io.EOF {
			return parts
		}
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(p)
		if err != nil {
			t.Fatal(err)
		}
		ct, _, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
		parts = append(parts, part{p.FileName(), ct, string(body)})
	}
}

var env = JoinEnv{
	JoinToken:      "dG9rZW4=",
	NodeName:       "default-abcde",
	RegisterTaints: []string{"karpenter.sh/unregistered:NoExecute", "dedicated=gpu:NoSchedule"},
}

func TestUserData_PartOrder(t *testing.T) {
	ud, err := UserData(env, Parts{BeforeJoin: "#cloud-config\npackages: [curl]\n", AfterJoin: "#!/bin/sh\necho after\n"}, "v1.34.1+k0s.0")
	if err != nil {
		t.Fatal(err)
	}
	parts := parseUserData(t, ud)
	var got []string
	for _, p := range parts {
		got = append(got, p.filename+" "+p.contentType)
	}
	want := []string{
		"join-env text/cloud-boothook",
		"10-before-join text/plain",
		"z10-join text/plain",
		"z20-after-join text/plain",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("parts:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if parts[1].content != "#cloud-config\npackages: [curl]\n" {
		t.Errorf("before-join content changed: %q", parts[1].content)
	}
	if !strings.Contains(parts[2].content, "K0S_VERSION=v1.34.1+k0s.0 sh") {
		t.Errorf("generated join does not install the requested k0s version:\n%s", parts[2].content)
	}
}

// TestUserData_ScriptsSortAroundRuncmd pins the filenames against the
// "runcmd" script cloud-init writes beside them and runs in lexical order.
func TestUserData_ScriptsSortAroundRuncmd(t *testing.T) {
	if filenameBeforeJoin >= "runcmd" || "runcmd" >= filenameJoin || filenameJoin >= filenameAfterJoin {
		t.Fatalf("want %s < runcmd < %s < %s", filenameBeforeJoin, filenameJoin, filenameAfterJoin)
	}
}

func TestUserData_JoinOverride(t *testing.T) {
	ud, err := UserData(env, Parts{Join: "#!/bin/sh\nkubeadm join\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	parts := parseUserData(t, ud)
	if len(parts) != 2 || parts[1].filename != "z10-join" || parts[1].content != "#!/bin/sh\nkubeadm join\n" {
		t.Errorf("parts = %+v, want join-env and the override", parts)
	}
}

func TestUserData_BadK0sVersion(t *testing.T) {
	for _, v := range []string{"", "v1.34.1", "v1.34.1+k0s", "1.34.1+k0s.0", "v1.34.1+k0s.0; rm -rf /"} {
		if _, err := UserData(env, Parts{}, v); err == nil {
			t.Errorf("k0s version %q accepted", v)
		}
	}
}

func TestJoinEnvBoothook(t *testing.T) {
	got := joinEnvBoothook(JoinEnv{JoinToken: "a'b", NodeName: "n", RegisterTaints: env.RegisterTaints})
	for _, want := range []string{
		"#cloud-boothook\n#!/bin/sh\n",
		"[ -e /etc/karpenter-hetzner/join.env ] && exit 0\n",
		"mkdir -p /etc/karpenter-hetzner\n",
		`JOIN_TOKEN='a'\''b'` + "\n",
		"NODE_NAME='n'\n",
		"REGISTER_TAINTS='karpenter.sh/unregistered:NoExecute,dedicated=gpu:NoSchedule'\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("boothook lacks %q:\n%s", want, got)
		}
	}
}
