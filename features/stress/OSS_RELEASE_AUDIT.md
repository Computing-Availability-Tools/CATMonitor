# Stress OSS release audit

## Scope

The repository audit checks only evidence owned by this feature:

- CATMonitor root license;
- bundled NPU Burn upstream identity, license files, and per-file checksums;
- NPU runtime-image license copy rule;
- the machine-readable runtime package list;
- optional CPU and NPU build manifest presence and schema.

Run the repository audit with:

```bash
make audit-stress-release
```

For a deployment bundle, also supply the manifests used by the deployment:

```bash
bash scripts/stress/audit_stress_release.sh \
  --cpu-manifest /opt/catmonitor/benchmarks/manifests/build-manifest.json \
  --npu-manifest /var/tmp/catmonitor-npu-build/manifests/npu-burn-image-manifest.json \
  --require-runtime-manifests
```

## Release gate

The repository-side gate passes only when the bundled material remains
traceable and its recorded license files remain intact. A product/appliance
release that includes external CPU assets, MPI/OpenBLAS, an NPU base image, or
OS packages is not closed by this check. That release must generate an SBOM
from the final delivered filesystem/image and package its exact third-party
licenses and notices.

The audit deliberately does not:

- fetch license data from the network;
- infer licenses from filenames or package names;
- declare administrator-provided binaries compliant;
- treat a build manifest as an SBOM;
- store node addresses, credentials, registry tokens, or proxy values.

The current repository therefore has a checked, explicit provenance boundary;
final redistribution compliance remains a release-owner gate for the concrete
artifact being shipped.
