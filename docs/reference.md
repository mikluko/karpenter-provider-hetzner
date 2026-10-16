# Reference

## HCloudNodeClass

`HCloudNodeClass` is cluster-scoped, in API group `karpenter.hetzner.cloud/v1`.

HCloudNodeClass spec fields:

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `locations` | `[]string` | yes | None | Hetzner locations to create servers in. At least one. |
| `networkID` | `int64` | no | None | ID of the private network every server attaches to. Without it, servers have only their public addresses, so `enablePublicIPv4` or `enablePublicIPv6` must stay `true`. |
| `imageSelector.family` | `debian` | yes | None | OS image family. The only value. |
| `imageSelector.version` | `string` | no | Newest Debian image | Substring matched against the image description, for example `13` for Debian 13. |
| `imageSelector.selector` | `map[string]string` | no | None | hcloud labels an image must carry, all of them, for example `{"role": "k0s-worker"}`. Hetzner's Debian images carry no labels, so a selector picks the newest matching snapshot of your own. |
| `firewallIDs` | `[]int64` | no | None | Firewalls attached to every server. |
| `sshKeyIDs` | `[]int64` | no | None | SSH keys installed on every server. |
| `placementGroupStrategy` | `spread` or `none` | no | `spread` | `spread` places servers in a spread placement group, on separate physical hosts. `none` uses no placement group. |
| `enablePublicIPv4` | `bool` | no | `true` | Attach a public IPv4. Hetzner bills it separately. |
| `enablePublicIPv6` | `bool` | no | `true` | Attach a public IPv6. |
| `labels` | `map[string]string` | no | None | Extra hcloud labels on every server. |
| `bootstrap` | object | yes | None | How a server joins the cluster. See [bootstrap](#bootstrap). |
| `kubelet` | object | no | See [kubelet](#kubelet) | Kubelet reservations the bootstrap applies. |

### bootstrap

`bootstrap` is the cloud-init a server boots with: the provider's `join.env`,
then `beforeJoin`, the join, and `afterJoin`. [About node bootstrap](about-node-bootstrap.md)
explains the parts and the order they run in.

bootstrap fields:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `beforeJoin` | part | no | Runs before the join. |
| `join` | part | no | Replaces the generated k0s join. |
| `afterJoin` | part | no | Runs after the join. |
| `k0sVersion` | `string` | unless `join` is set | k0s release tag the generated join installs, such as `v1.34.1+k0s.0`. |

A part is one cloud-init document, such as a shell script or a cloud-config,
given exactly one way:

| Field | Type | Description |
|-------|------|-------------|
| `inline` | `string` | The document. |
| `secretRef` | `{namespace, name, key}` | Secret key holding the document. Read when a server is created. |

`join.env` variables, in `/etc/karpenter-hetzner/join.env`:

| Variable | Value |
|----------|-------|
| `JOIN_TOKEN` | k0s worker join token for the node's bootstrap token. |
| `NODE_NAME` | NodeClaim name, also the server's hostname. |
| `REGISTER_TAINTS` | Comma-separated taints for `--register-with-taints`. |
| `KUBELET_EXTRA_ARGS` | Not written by the provider. A `beforeJoin` part may append it; the generated join passes it to the kubelet. |

### kubelet

`kubelet` describes what the bootstrap configures on the kubelet. The provider
does not configure the kubelet; it subtracts these values from each server type's
capacity to compute allocatable. Declaring less than the bootstrap reserves
leaves pods Pending on nodes too small for them.

Without `kubelet`, or with it empty, the provider subtracts a `kubeReserved` of
100m CPU and 100Mi memory. Once `kubelet` declares any value, only the declared
values apply, plus the eviction defaults below.

kubelet fields:

| Field | Keys | Value |
|-------|------|-------|
| `systemReserved` | `cpu`, `memory`, `ephemeral-storage`, `pid` | A quantity, for example `200m` or `512Mi`. |
| `kubeReserved` | `cpu`, `memory`, `ephemeral-storage`, `pid` | A quantity. |
| `evictionHard` | `memory.available`, `allocatableMemory.available`, `nodefs.available`, `nodefs.inodesFree`, `imagefs.available`, `imagefs.inodesFree`, `containerfs.available`, `containerfs.inodesFree`, `pid.available` | A quantity, or a percentage of capacity such as `10%`. The value alone, without the kubelet flag's `<`. |

Of the `evictionHard` signals, only `memory.available` and `nodefs.available`
reduce allocatable; the others are accepted and have no effect. Either one left
out takes the kubelet default: `memory.available` 100Mi, `nodefs.available` 10%.
Declare `0%` for a signal your kubelet does not enforce.

The kubelet's own view of these values on a running node:

```bash
kubectl get --raw /api/v1/nodes/<node-name>/proxy/configz \
  | jq '.kubeletconfig | {systemReserved, kubeReserved, evictionHard}'
```

### Status

HCloudNodeClass status fields:

| Field | Description |
|-------|-------------|
| `conditions` | `ImagesReady`, `LocationsReady`, `NetworkReady`, `ResourcesReady`, and `UserDataReady`, aggregated into `Ready`. `UserDataReady` is False while a bootstrap part's Secret or key is missing. |
| `resolvedImages` | One image ID per hcloud architecture, `x86` or `arm`. |

## Node labels

Labels this provider sets on nodes, in addition to the Karpenter well-known labels:

| Label | Example | Value |
|-------|---------|-------|
| `topology.kubernetes.io/zone` | `nbg1` | Hetzner location |
| `node.kubernetes.io/instance-type` | `cax21` | Hetzner server type |
| `karpenter.hetzner.cloud/server-family` | `cax` | Server type family: `cx`, `cpx`, `cax`, or `ccx` |
| `karpenter.hetzner.cloud/cpu-type` | `shared` | `shared` or `dedicated` |

## Controller environment

Environment variables the controller reads. The Helm chart sets each from its values.

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `HCLOUD_TOKEN` | yes | None | Hetzner Cloud API token. |
| `CLUSTER_NAME` | yes | None | Cluster name. The controller manages only servers labelled with it. |
| `API_SERVER_URL` | yes | None | `https` address of the API server that new nodes join through. |
| `HCLOUD_API_TIMEOUT` | no | `30s` | Timeout of a single hcloud API request, as a Go duration. At most `5m`. |
| `METRICS_PORT` | no | `8080` | Prometheus metrics port. |
| `HEALTH_PROBE_PORT` | no | `8081` | Health and readiness probe port. |
