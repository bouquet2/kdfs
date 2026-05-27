> [!CAUTION]
> KDFS, while tested thoroughly, is still a recent project with no production workloads running on it (yet). We do not take any responsibility for lost-data, broken clusters, unbound PVCs, a nuclear bomb exploding inside the cluster, the works.

# KDFS
**K**reato's **D**istributed **F**ile **S**ystem is a cloud-native, modern and distributed storage system that prioritizes being modern and being simple above all else. The name is actually somewhat of a lie, it is not an actual filesystem (yet), but it does manage filesystems.

KDFS supports all PV persistent modes (RWO, ROX, RWX, RWOP).

It primarily uses NVMe-oF (RWO, ROX, RWOP) but also has NFS for RWX as there is not a better alternative at the moment (other than CephFS which is *much* heavier, see Comparison for more info).


## Features
* Uses modern Go, no hacks/external commands as much as possible.
* Extremely simple to install and use for most workloads.
* Supposedly very fast as it uses latest and greatest such as NVMe-oF and XFS (not benchmarked yet, but it should be faster than iSCSI that is currently the standard for such workloads)
* Uses Replica + Local system unlike the likes of Longhorn that *should* enhance read speeds

## Installation

From the local chart in this repository:

```bash
helm install kdfs charts/kdfs --namespace kdfs --create-namespace
```

From the published OCI chart on GHCR:

```bash
helm install kdfs oci://ghcr.io/bouquet2/kdfs/kdfs \
  --version 0.1.0 \
  --namespace kdfs \
  --create-namespace
```

To override the NQN authority:

```bash
helm install kdfs charts/kdfs \
  --namespace kdfs \
  --create-namespace \
  --set config.nqnAuthority=nqn.2026-05.example.com
```

The same override works with the OCI chart:

```bash
helm install kdfs oci://ghcr.io/bouquet2/kdfs/kdfs \
  --version 0.1.0 \
  --namespace kdfs \
  --create-namespace \
  --set config.nqnAuthority=nqn.2026-05.example.com
```


## Comparison

* Rook-Ceph: Much heavier than any other solution, too complex for most usecases
* Longhorn: Still uses iSCSI ([though that might change soon](https://docs.harvesterhci.io/v1.8/advanced/longhorn-v2/)), very fragile (in my experience, YMMV), hell to debug, uses ext4 which is generally considered slower than XFS
* OpenEBS/Mayastor: Pretty similar to this project but uses Rust and more complex to set up, uses ext4 which is generally considered slower than XFS

Honestly Rook-Ceph is still your best bet (if you can afford the hardware requirements, and you have the mental capacity to set it up) but not everybody can, so here we go.

Keep in mind that unlike all of those solutions listed above KDFS prioritizes RWO, ROX and RWOP workloads as they run with NVMe-oF instead of NFS which should be much faster while avoiding issues. RWX basically exists as an edge-case anyway.



## License
KDFS is free software: you can redistribute it and/or modify it under the terms of the GNU Affero General Public License as published by the Free Software Foundation.

KDFS is distributed in the hope that it will be useful, but WITHOUT ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License along with KDFS. If not, see https://www.gnu.org/licenses/.
