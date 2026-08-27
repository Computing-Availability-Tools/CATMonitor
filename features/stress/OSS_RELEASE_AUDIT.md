# Stress OSS release audit

## Scope

The repository audit checks only evidence owned by this feature:

- CATMonitor root license;
- bundled NPU Burn upstream identity, license files, and per-file checksums;
- NPU runtime-image license copy rule;
- the machine-readable runtime package list;
- optional CPU and NPU build manifest presence and schema.
- the optional CPU workload image/deployment security boundary.

Run the repository audit with:

```bash
make audit-stress-release
```

For a deployment bundle, also supply the manifests used by the deployment:

```bash
bash scripts/stress/audit_stress_release.sh \
  --cpu-manifest /opt/catmonitor/stress/manifests/cpu-build-manifest.json \
  --npu-manifest /var/tmp/catmonitor-npu-build/manifests/npu-burn-image-manifest.json \
  --require-runtime-manifests
```

New CPU build manifests encode `schema_version` as a positive JSON integer.
The audit also accepts a quoted positive integer for compatibility with older
CPU manifests and the current NPU image manifest schema. Zero, missing, and
non-numeric versions are rejected.

## Release gate

The repository-side gate passes only when the bundled material remains
traceable and its recorded license files remain intact. A product/appliance
release that includes external CPU assets, MPI/OpenBLAS, an NPU base image, or
OS packages is not closed by this check. That release must generate an SBOM
from the final delivered filesystem/image and package its exact third-party
licenses and notices.

`build_cpu_runner_image.sh` consumes administrator-provided CPU benchmark
sources and creates a local workload image; the CATMonitor repository does not vendor
those CPU sources or publish the resulting image. If that image is distributed,
its image manifest can be passed as `--cpu-manifest`, but the
release owner must still inventory Debian, MPI, OpenBLAS and benchmark licenses
from the concrete final image.

The audit deliberately does not:

- fetch license data from the network;
- infer licenses from filenames or package names;
- declare administrator-provided binaries compliant;
- treat a build manifest as an SBOM;
- store node addresses, credentials, registry tokens, or proxy values.

The current repository therefore has a checked, explicit provenance boundary;
final redistribution compliance remains a release-owner gate for the concrete
artifact being shipped.
