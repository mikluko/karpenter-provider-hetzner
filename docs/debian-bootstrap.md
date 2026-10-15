# Join Debian Nodes with kubeadm

This guide shows you how to have Karpenter create Debian 13 (trixie) workers that join an
existing kubeadm cluster through cloud-init. See
[About node bootstrap](about-node-bootstrap.md) for what any bootstrap has to
provide.

[`examples/debian-nodeclass.yaml`](../examples/debian-nodeclass.yaml) holds the
node class and node pool, with a cloud-init document that:

1. loads the kernel modules and sysctl settings Kubernetes needs;
1. installs containerd from the Debian archive, with the systemd cgroup driver;
1. installs kubeadm, the kubelet, and kubectl from the Kubernetes apt repository;
1. registers the kubelet with the `karpenter.sh/unregistered` taint;
1. runs `kubeadm join`.

## Before you begin

- The provider is installed, as the [README](../README.md#install) describes.
- You have shell access to a control-plane node.

## Create a join command

On a control-plane node:

```bash
kubeadm token create --print-join-command
```

The output is similar to this:

```
kubeadm join 10.0.0.2:6443 --token abcdef.0123456789abcdef --discovery-token-ca-cert-hash sha256:<hash>
```

The token expires after 24 hours by default. Every server Karpenter creates after
that fails to join, so create a token with a longer `--ttl`, or replace the token
before it expires.

## Put the join command in the node class

1. In `examples/debian-nodeclass.yaml`, replace `<CONTROL_PLANE_ENDPOINT>`,
   `<BOOTSTRAP_TOKEN>`, and `<CA_CERT_HASH>` with the values from the join
   command, and set `networkID` and `locations` to your own.

1. Optional: keep the token out of git. Save the `userData` document to
   `cloud-init.yaml`, store it in a Secret, and reference the Secret from the
   node class in place of `userData`:

   ```bash
   kubectl create secret generic debian-worker-userdata \
     --namespace kube-system \
     --from-file=cloud-init.yaml=cloud-init.yaml
   ```

   ```yaml
   userDataSecretRef:
     namespace: kube-system
     name: debian-worker-userdata
     key: cloud-init.yaml
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
console, and read the cloud-init and kubelet logs:

```bash
cat /var/log/cloud-init-output.log
journalctl --unit kubelet --no-pager | tail -40
```

SSH over the public network needs `enablePublicIPv4: true` and a key in
`sshKeyIDs`.

## Declare kubelet reservations

Once a node has joined, copy the reservations its kubelet applies into the node
class's `spec.kubelet`, as the [reference](reference.md#kubelet) describes.
