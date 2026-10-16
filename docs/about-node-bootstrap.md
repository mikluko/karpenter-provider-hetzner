# About node bootstrap

This provider creates servers and leaves joining them to the cluster to you.
Each server boots Debian with the `userData` of its `HCloudNodeClass`, a
cloud-init document. Whatever that document does is the whole of the bootstrap.
The provider neither renders it nor checks it, so the same provider joins
kubeadm, k0s, or k3s nodes, or anything else cloud-init can install.

[Join Debian nodes with kubeadm](debian-bootstrap.md) walks through one such
document. This page covers what any of them has to provide.

## What Karpenter expects of a new node

Karpenter picks a server type and location, then records the labels that choice
implies on the NodeClaim: `karpenter.sh/nodepool`,
`node.kubernetes.io/instance-type`, `karpenter.sh/capacity-type`, and
`topology.kubernetes.io/zone`. Karpenter core copies them onto the Node once it
registers, so a bootstrap gains nothing by setting them, and loses something by
setting them wrong.

The bootstrap has to supply two things the NodeClaim cannot.

The first is the provider ID. Karpenter finds the NodeClaim of a registering Node
by `spec.providerID`, which has to read `hcloud://<server-id>`. The hcloud Cloud
Controller Manager sets it on a cluster that runs one. On a cluster without it,
the kubelet sets it at registration, with the server ID from the metadata
service:

```bash
--provider-id=hcloud://$(curl -s http://169.254.169.254/hetzner/v1/metadata/instance-id)
```

The second is a taint. Karpenter expects a new
node to register with `karpenter.sh/unregistered=:NoExecute`, and removes it once
the NodeClaim's labels, taints, and owner references are on the Node. Until then
the taint keeps pods off a node Karpenter has not finished with. On any
distribution the kubelet flag
`--register-with-taints=karpenter.sh/unregistered=:NoExecute` sets it.

A missing taint does not fail. Karpenter core emits an `UnregisteredTaintMissing`
event on the NodeClaim and carries on, and the only symptom is a pod that
occasionally lands on a node before its labels and taints arrive. That is a race,
and it does not reproduce on demand.

## The zone label and the hcloud CCM

The hcloud Cloud Controller Manager also writes `topology.kubernetes.io/zone`
when it initializes a node, and fills it with a datacenter name such as
`nbg1-dc3`. This provider's offerings are keyed on the location, `nbg1`. A node
whose zone carries the datacenter matches no offering, which prices it at zero
and keeps it from ever being consolidated. From version 1.35.0 the CCM leaves the
label alone when it runs with `HCLOUD_INSTANCES_ZONE_LABEL_ENABLED=false`.

## Kubelet reservations

Karpenter decides whether a pod fits a server type by the type's allocatable
resources, which it computes the way the kubelet does: capacity less
`kubeReserved`, `systemReserved`, and the hard eviction thresholds. Only the
bootstrap knows what those are, so the node class has to be told, in
`spec.kubelet`. When the two disagree the scheduler and the kubelet disagree too,
and pods go to nodes that cannot admit them. The [reference](reference.md#kubelet)
lists the fields and shows how to read the values off a running node.

## userData is readable from the node

`userDataSecretRef` keeps a join credential out of git and out of the node class.
It does not keep it off the node. Hetzner serves every server's userData from the
metadata service at `169.254.169.254`, so any process on the node can read the
credential the node joined with, and so can any pod that can reach the link-local
address. A bootstrap rendered by the provider would not change this: cloud-init
has to read the credential from somewhere the node can reach.

Two measures limit the exposure: give the bootstrap the narrowest credential
that still joins a worker, and block pod egress to `169.254.169.254/32`.

