// Copyright The karpenter-provider-hetzner Authors.
// SPDX-License-Identifier: Apache-2.0

// Package bootstrap builds the userData a server joins the cluster with: the
// join credential, the join.env file, and the cloud-init parts around the join.
package bootstrap

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	bootstrapapi "k8s.io/cluster-bootstrap/token/api"
	bootstraputil "k8s.io/cluster-bootstrap/token/util"
	karpapis "sigs.k8s.io/karpenter/pkg/apis"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	apiv1 "github.com/mikluko/karpenter-provider-hetzner/pkg/apis/v1"
)

// LabelNodeClaim marks a bootstrap token Secret with the NodeClaim it was minted for.
const LabelNodeClaim = apiv1.Group + "/nodeclaim"

// NewTokenSecret mints a bootstrap token for nodeClaim and returns it with the
// kube-system Secret that makes the API server accept it. The Secret expires
// after ttl and is owned by nodeClaim, so the garbage collector deletes it with
// the NodeClaim. nodeClaim must have a UID.
func NewTokenSecret(nodeClaim *karpv1.NodeClaim, now time.Time, ttl time.Duration) (string, *corev1.Secret, error) {
	if nodeClaim.UID == "" {
		return "", nil, fmt.Errorf("nodeclaim %s has no UID to own its bootstrap token", nodeClaim.Name)
	}
	token, err := bootstraputil.GenerateBootstrapToken()
	if err != nil {
		return "", nil, fmt.Errorf("generating bootstrap token: %w", err)
	}
	id, secret := token[:bootstrapapi.BootstrapTokenIDBytes], token[bootstrapapi.BootstrapTokenIDBytes+1:]
	return token, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: metav1.NamespaceSystem,
			Name:      bootstraputil.BootstrapTokenSecretName(id),
			Labels:    map[string]string{LabelNodeClaim: nodeClaim.Name},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: karpapis.Group + "/v1",
				Kind:       "NodeClaim",
				Name:       nodeClaim.Name,
				UID:        nodeClaim.UID,
			}},
		},
		Type: bootstrapapi.SecretTypeBootstrapToken,
		StringData: map[string]string{
			bootstrapapi.BootstrapTokenDescriptionKey:      "Worker join token for NodeClaim " + nodeClaim.Name,
			bootstrapapi.BootstrapTokenIDKey:               id,
			bootstrapapi.BootstrapTokenSecretKey:           secret,
			bootstrapapi.BootstrapTokenExpirationKey:       now.Add(ttl).UTC().Format(time.RFC3339),
			bootstrapapi.BootstrapTokenUsageAuthentication: "true",
		},
	}, nil
}

// K0sJoinToken encodes token as a k0s worker join token: a kubeconfig for
// server, trusting caData, gzipped and base64-encoded the way `k0s token create`
// does.
func K0sJoinToken(server string, caData []byte, token string) (string, error) {
	const name = "k0s"
	kubeconfig, err := clientcmd.Write(clientcmdapi.Config{
		Clusters:       map[string]*clientcmdapi.Cluster{name: {Server: server, CertificateAuthorityData: caData}},
		Contexts:       map[string]*clientcmdapi.Context{name: {Cluster: name, AuthInfo: "kubelet-bootstrap"}},
		CurrentContext: name,
		AuthInfos:      map[string]*clientcmdapi.AuthInfo{"kubelet-bootstrap": {Token: token}},
	})
	if err != nil {
		return "", fmt.Errorf("writing join kubeconfig: %w", err)
	}
	var buf bytes.Buffer
	gz, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return "", err
	}
	if _, err := gz.Write(kubeconfig); err != nil {
		return "", err
	}
	if err := gz.Close(); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}
