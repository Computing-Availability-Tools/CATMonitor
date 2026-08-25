# Stress third-party notices

This file records third-party material distributed by the CATMonitor stress
feature and the boundary for administrator-supplied benchmark assets. It is
not legal advice.

## Material distributed in this repository

### MindCluster AscendNPUBurn

- Component: MindCluster AscendNPUBurn
- Location: `third_party/ascend_npu_burn/source`
- Recorded upstream and revision: `third_party/ascend_npu_burn/UPSTREAM`
- Audit notes: `third_party/ascend_npu_burn/UPSTREAM.md`
- Software license: Mulan PSL v2
- Software license text: `third_party/ascend_npu_burn/source/LICENSE.md`
- Documentation license: CC BY 4.0, as declared by that upstream revision
- Documentation license text: `third_party/ascend_npu_burn/source/docs/LICENSE`
- Integrity manifest: `third_party/ascend_npu_burn/SOURCE_SHA256SUMS`

The NPU Burn runtime image copies the software license into
`/usr/share/licenses/ascend-npu-burn/LICENSE.md`. CATMonitor compatibility
patches remain outside the recorded upstream source tree.

## Administrator-supplied material

CATMonitor does not distribute STREAM, HPL, HPCG, OpenBLAS, MPI, CANN,
torch_npu, base container images, or `pciutils` binaries as part of this
repository. The build/deployment tools consume assets selected by an
administrator and record technical provenance in manifests.

If a release or appliance redistributes any of those sources, binaries, base
images, packages, or their derivatives, its publisher must independently:

1. identify the exact component versions and sources;
2. retain and deliver all applicable license and notice material;
3. generate an SBOM for the actual delivered artifact;
4. review source-offer, attribution, patent, export, and redistribution terms;
5. preserve the CPU build, NPU image, and stress deployment manifests.

The CATMonitor manifests and `stress doctor` report what was built and what is
currently executable. They are not a license determination or a complete SBOM.
