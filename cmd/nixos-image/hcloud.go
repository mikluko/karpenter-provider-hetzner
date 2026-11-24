// Copyright The karpenter-provider-hetzner Authors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// hcloudAPI is the cloud a build runs against.
type hcloudAPI struct{ c *hcloud.Client }

func (a hcloudAPI) ServerTypes(ctx context.Context) ([]*hcloud.ServerType, error) {
	return a.c.ServerType.All(ctx)
}

func (a hcloudAPI) CreateSSHKey(ctx context.Context, name, publicKey string, labels map[string]string) (*hcloud.SSHKey, error) {
	key, _, err := a.c.SSHKey.Create(ctx, hcloud.SSHKeyCreateOpts{Name: name, PublicKey: publicKey, Labels: labels})
	return key, err
}

func (a hcloudAPI) DeleteSSHKey(ctx context.Context, key *hcloud.SSHKey) error {
	_, err := a.c.SSHKey.Delete(ctx, key)
	return err
}

func (a hcloudAPI) CreateServer(ctx context.Context, opts hcloud.ServerCreateOpts) (*hcloud.Server, error) {
	res, _, err := a.c.Server.Create(ctx, opts)
	if err != nil {
		return nil, err
	}
	if err := a.c.Action.WaitFor(ctx, append([]*hcloud.Action{res.Action}, res.NextActions...)...); err != nil {
		return res.Server, err
	}
	return res.Server, nil
}

func (a hcloudAPI) PowerOff(ctx context.Context, server *hcloud.Server) error {
	action, _, err := a.c.Server.Poweroff(ctx, server)
	if err != nil {
		return err
	}
	return a.c.Action.WaitFor(ctx, action)
}

func (a hcloudAPI) Snapshot(ctx context.Context, server *hcloud.Server, description string, labels map[string]string) (*hcloud.Image, error) {
	res, _, err := a.c.Server.CreateImage(ctx, server, &hcloud.ServerCreateImageOpts{
		Type:        hcloud.ImageTypeSnapshot,
		Description: new(description),
		Labels:      labels,
	})
	if err != nil {
		return nil, err
	}
	if err := a.c.Action.WaitFor(ctx, res.Action); err != nil {
		return nil, err
	}
	return res.Image, nil
}

func (a hcloudAPI) DeleteServer(ctx context.Context, server *hcloud.Server) error {
	res, _, err := a.c.Server.DeleteWithResult(ctx, server)
	if err != nil {
		return err
	}
	return a.c.Action.WaitFor(ctx, res.Action)
}
