# About node bootstrap

Each server boots Debian with a cloud-init document this provider builds from the
`bootstrap` of its `HCloudNodeClass`. The document has four parts, in order:

1. `join.env`, written by the provider: the node's join token and the values the
   join needs.
1. `beforeJoin`, yours: a VPN, packages, kubelet arguments.
1. The join. By default the provider generates one that installs k0s and starts
   a worker. `join` replaces it.
1. `afterJoin`, yours.

[Join Debian nodes to a k0s cluster](debian-bootstrap.md) walks through a node
class that uses the generated join. This page covers what the parts can rely on,
and what a replaced join has to provide.

## join.env

`/etc/karpenter-hetzner/join.env` is a shell file, readable only by root, that
every part can source. The provider writes it before any other part runs:

| Variable | Value |
|----------|-------|
| `JOIN_TOKEN` | A k0s worker join token: a kubeconfig for the API server in `apiServerURL`, holding the node's bootstrap token, gzipped and base64-encoded. |
| `NODE_NAME` | The NodeClaim name, which is also the server's hostname. |
| `REGISTER_TAINTS` | The taints the kubelet registers with, comma-separated: `karpenter.sh/unregistered:NoExecute`, then the NodeClaim's taints and startup taints. |

A `beforeJoin` part may append `KUBELET_EXTRA_ARGS` to the file, and the
generated join passes them to the kubelet:

```bash
echo 'KUBELET_EXTRA_ARGS="--max-pods=60"' >> /etc/karpenter-hetzner/join.env
```

The provider writes the file on first boot only, so what a part appends
survives a reboot.

## The join token

The provider creates a bootstrap token for every NodeClaim, as a Secret in
`kube-system` owned by the NodeClaim. The token authenticates as a member of
`system:bootstrappers`, expires after an hour, and is deleted with its
NodeClaim. No long-lived worker credential sits in a Secret or in the node
class.

The token is still readable from the node. Hetzner serves every server's
userData from the metadata service at `169.254.169.254`, so any process on the
node can read the token the node joined with, and so can any pod that can reach
the link-local address. Block pod egress to `169.254.169.254/32` to close the
second path.

## Part order

cloud-init runs every shell-script part, and the script it writes for a
cloud-config's `runcmd`, from one directory in filename order. The provider
names the parts so that the order is:

1. a `beforeJoin` shell script;
1. the `runcmd` of every cloud-config part;
1. the join;
1. an `afterJoin` shell script.

A cloud-config `beforeJoin` therefore runs its `packages`, `write_files` and
`runcmd` before the join. A cloud-config `afterJoin` does not run after the
join, and cloud-init drops each of its keys that a cloud-config `beforeJoin`
already sets. Write `afterJoin` as a shell script.

## What Karpenter expects of a new node

Karpenter picks a server type and location, then records the labels that choice
implies on the NodeClaim: `karpenter.sh/nodepool`,
`node.kubernetes.io/instance-type`, `karpenter.sh/capacity-type`, and
`topology.kubernetes.io/zone`. Karpenter core copies them onto the Node once it
registers, so a bootstrap gains nothing by setting them, and loses something by
setting them wrong.

The join has to supply two things the NodeClaim cannot. The generated join
supplies both; a replaced join has to supply them itself.

The first is the provider ID. Karpenter finds the NodeClaim of a registering Node
by `spec.providerID`, which has to read `hcloud://<server-id>`. The kubelet sets
it at registration, with the server ID from the metadata service:

```bash
--provider-id=hcloud://$(curl -s http://169.254.169.254/hetzner/v1/metadata/instance-id)
```

The hcloud Cloud Controller Manager also sets it, on a cluster that runs one.

The second is the taints in `REGISTER_TAINTS`. Karpenter expects a new node to
register with `karpenter.sh/unregistered:NoExecute`, and removes it once the
NodeClaim's labels, taints, and owner references are on the Node. Until then the
taint keeps pods off a node Karpenter has not finished with. The kubelet flag is
`--register-with-taints="$REGISTER_TAINTS"`.

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
`kubeReserved`, `systemReserved`, and the hard eviction thresholds. The generated
join sets no reservations, so any the kubelet applies come from your parts, and
the node class has to be told, in `spec.kubelet`. When the two disagree the
scheduler and the kubelet disagree too, and pods go to nodes that cannot admit
them. The [reference](reference.md#kubelet) lists the fields and shows how to
read the values off a running node.
