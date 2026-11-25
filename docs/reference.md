# Reference

## HCloudNodeClass

`HCloudNodeClass` is cluster-scoped, in API group `karpenter.hetzner.cloud/v1`.

HCloudNodeClass spec fields:

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `locations` | `[]string` | yes | None | Hetzner locations to create servers in. At least one. |
| `networkID` | `int64` | no | None | ID of the private network every server attaches to. Without it, servers have only their public addresses, so `enablePublicIPv4` or `enablePublicIPv6` must stay `true`. |
| `imageSelector.family` | `debian` or `snapshot` | yes | None | Image family. `debian` boots Hetzner's Debian images or your snapshots; `snapshot` boots snapshots of your own only, of any OS, and requires `selector` or `ids`, and `bootstrap.join`. |
| `imageSelector.version` | `string` | no | Newest matching image | Substring matched against the image description, for example `13` for Debian 13. |
| `imageSelector.selector` | `map[string]string` | `snapshot`, unless `ids` is set | None | hcloud labels an image must carry, all of them, for example `{"role": "k0s-worker"}`. Hetzner's Debian images carry no labels, so a selector picks the newest matching snapshot of your own. Once a newer snapshot matches, the node class resolves it within five minutes, and nodes on the older one drift and are replaced within the NodePool's disruption budget; `budgets: [{nodes: "1"}]` replaces them one at a time. |
| `imageSelector.ids` | `[]int64` | `snapshot`, unless `selector` is set | None | Snapshot IDs to boot, at most one per architecture; an architecture none of them has gets no image. Nodes drift when the list changes. Not with `version`. |
| `firewallIDs` | `[]int64` | no | None | Firewalls attached to every server. |
| `sshKeyIDs` | `[]int64` | no | None | SSH keys installed on every server. |
| `placementGroupStrategy` | `spread` or `none` | no | `spread` | `spread` places servers in a spread placement group, on separate physical hosts. `none` uses no placement group. |
| `enablePublicIPv4` | `bool` | no | `true` | Attach a public IPv4. Hetzner bills it separately. |
| `enablePublicIPv6` | `bool` | no | `true` | Attach a public IPv6. |
| `labels` | `map[string]string` | no | None | Extra hcloud labels on every server. |
| `nodeNameTemplates` | `[]string` | no | `["{{ .NodeClaim.Name }}"]` | Templates for the name of a server, and so of its hostname and Node. See [nodeNameTemplates](#nodenametemplates). |
| `bootstrap` | object | yes | None | How a server joins the cluster. See [bootstrap](#bootstrap). |
| `kubelet` | object | no | See [kubelet](#kubelet) | Kubelet reservations the bootstrap applies. |

### nodeNameTemplates

Each template is a Go [text/template](https://pkg.go.dev/text/template) with
the [sprig](https://masterminds.github.io/sprig/) functions, except `env`,
`expandenv` and `getHostByName`. Every template is rendered when a server is
created, and the server takes one of the names at random, among those no Node
of the cluster and no server of the Hetzner project holds, whichever node
class or cluster it belongs to. With none free, the launch fails with
insufficient capacity. A template that does not render a lowercase DNS label
of at most 63 characters fails the launch. Changing the templates does not
drift nodes.

A template is given:

| Field | Value |
|-------|-------|
| `.NodeClass.Name`, `.NodeClass.Labels` | The node class's name and metadata labels. |
| `.NodeClaim.Name`, `.NodeClaim.Labels` | The NodeClaim's name and labels, `karpenter.sh/nodepool` among them. |
| `.Zone` | The Hetzner location the server is created in, such as `fsn1`. |
| `.Rand` | A random non-negative `int64`, drawn for each template. |

A template without actions is a name of its own. For example, these templates
name a server `bugoga-fsn1-` and six random hex digits, or `pushkin` while no
Node or server holds that name:

```yaml
nodeNameTemplates:
  - 'bugoga-{{ .Zone }}-{{ .Rand | printf "%016x" | trunc 6 }}'
  - pushkin
```

### bootstrap

`bootstrap` is the cloud-init a server boots with: the provider's `join.env`,
then `beforeJoin`, the join, and `afterJoin`. [About node bootstrap](about-node-bootstrap.md)
explains the parts and the order they run in.

bootstrap fields:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `beforeJoin` | part | no | Runs before the join. |
| `join` | part | `snapshot` | Replaces the generated k0s join. A `snapshot` node class has no generated join. |
| `afterJoin` | part | no | Runs after the join. |
| `k0sVersion` | `string` | `debian`, unless `join` is set | k0s release tag the generated join installs, such as `v1.34.1+k0s.0`. A `snapshot` node class refuses it. |

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
| `NODE_NAME` | Server name, also its hostname, rendered from `nodeNameTemplates`. |
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
