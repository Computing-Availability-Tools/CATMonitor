# Ascend NPU Burn upstream record

The files under `source/` are vendored from the official
MindCluster AscendNPUBurn repository so a CATMonitor source archive contains
the NPU stress implementation required by `features/stress`.

| Field | Value |
|---|---|
| Repository | `https://gitcode.com/Ascend/MindCluster-AscendNPUBurn.git` |
| Revision | `381028b688a70e881d97477d7fa1ae8f2a26288e` |
| Git tree | `2a4b9562831a900f7b2925745c9f87549eb677e2` |
| Tag context | The revision is contained in upstream tag `v26.1.0`; it is not the tag's peeled commit |
| Synchronization date | `2026-08-10` |
| Deterministic `git archive` SHA-256 | `46bb4255001582411758f2ac3b63328c2307d13fa802dc084c8db6d89f4c49ac` |
| `SOURCE_SHA256SUMS` SHA-256 | `53620b941563a860b53868685e3c09ea14709f7a0f9a86c7ca5c880cb7f913b5` |
| License | Mulan PSL v2 (`source/LICENSE.md`) |
| Upstream NOTICE | No standalone root `NOTICE` file exists at the recorded revision; upstream copyright and license statements are retained in place |
| Direct CATMonitor modifications | None |

The source tree was compared byte-for-byte with the Git blobs at the recorded
revision. A previously available ZIP contained nine additional legacy
documentation files that were not tracked by this revision; those files were
not imported because they could not be attributed to the fixed Git tree.

Mulan PSL v2 permits distribution in source or executable form when recipients
receive a copy of the license and the copyright, patent, trademark, and
disclaimer statements are retained. The vendored component remains under its
upstream license and is not relicensed under the CATMonitor Apache-2.0 license.

CATMonitor compatibility changes must not be edited into `source/`. Store an
audited patch under `scripts/stress/patches/ascend_npu_burn/` and apply it only
to the isolated build snapshot. The initial A3 build profile is `none`, which
applies no patch.

When updating upstream:

1. review the new revision and its license/notice files;
2. replace `source/` with an exact archive of one immutable commit;
3. update both this document and the machine-readable `UPSTREAM` file;
4. run the NPU image-builder DFX tests and A2/A3 compatibility acceptance;
5. publish the change only after the runtime image and cleanup paths pass.
