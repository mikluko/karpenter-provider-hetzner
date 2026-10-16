# Join Debian Nodes to a k0s Cluster

This guide shows you how to have Karpenter create Debian 13 (trixie) workers
that join an existing k0s cluster with the join the provider generates. See
[About node bootstrap](about-node-bootstrap.md) for how the parts of a bootstrap
fit together, and for what a join of your own has to provide.

[`examples/debian-nodeclass.yaml`](../examples/debian-nodeclass.yaml) holds the
node class and node pool.

## Before you begin

- The provider is installed, as the [README](../README.md#install) describes,
  with `apiServerURL` set to an address of the API server that a new server can
  reach before it joins, such as a control-plane address on the private network.
- You have shell access to a control-plane node.

## Find the k0s version

The generated join installs the k0s release you name, and it should match the
control plane's. On a control-plane node:

```bash
sudo k0s version
```

The output is a release tag similar to `v1.34.1+k0s.0`.

## Write the node class

1. In `examples/debian-nodeclass.yaml`, set `bootstrap.k0sVersion` to that tag,
   and set `networkID` and `locations` to your own.

1. Optional: give the node a part that runs before the join. A `beforeJoin`
   part can install packages, bring up a VPN, or pass extra arguments to the
   kubelet through `join.env`:

   ```yaml
   bootstrap:
     k0sVersion: v1.34.1+k0s.0
     beforeJoin:
       inline: |
         #!/bin/sh
         echo 'KUBELET_EXTRA_ARGS="--max-pods=60"' >> /etc/karpenter-hetzner/join.env
   ```

1. Optional: keep a part out of git. Store it in a Secret, and reference the
   Secret in place of `inline`:

   ```bash
   kubectl create secret generic debian-worker-bootstrap \
     --namespace kube-system \
     --from-file=before-join.sh=before-join.sh
   ```

   ```yaml
   beforeJoin:
     secretRef:
       namespace: kube-system
       name: debian-worker-bootstrap
       key: before-join.sh
   ```

## Apply and verify

1. Apply the node class and node pool:

   ```bash
   kubectl apply -f examples/debian-nodeclass.yaml
   ```

1. Check that the node class is ready:

   ```bash
   kubectl get hcloudnodeclasses debian-default
   ```

1. Once a pod is pending, watch for the new node:

   ```bash
   kubectl get nodes --watch
   ```

If a node does not join, connect to the server over SSH or through the Hetzner
console, and read the cloud-init and k0s worker logs:

```bash
cat /var/log/cloud-init-output.log
journalctl --unit k0sworker --no-pager | tail -40
```

SSH over the public network needs `enablePublicIPv4: true` and a key in
`sshKeyIDs`.

## Replace the join

Set `bootstrap.join` to run a join of your own instead of the generated one;
`k0sVersion` is then not needed. The join sources
`/etc/karpenter-hetzner/join.env`, and has to set the node's provider ID and
register it with `REGISTER_TAINTS`, as
[About node bootstrap](about-node-bootstrap.md#what-karpenter-expects-of-a-new-node)
describes. `JOIN_TOKEN` is a k0s join token; this prints the kubeconfig inside
it, which holds the API server address, the cluster CA, and the bootstrap token:

```bash
echo "$JOIN_TOKEN" | base64 -d | gunzip
```

## Declare kubelet reservations

Once a node has joined, copy the reservations its kubelet applies into the node
class's `spec.kubelet`, as the [reference](reference.md#kubelet) describes.
