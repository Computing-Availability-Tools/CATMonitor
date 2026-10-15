# Ascend NPU Burn compatibility patches

Apply CATMonitor compatibility changes to an isolated build copy; keep
`third_party/ascend_npu_burn/source` complete and unchanged. Every patch is
explicit, ordered, and recorded with its SHA-256 in the build manifest.
Do not label a patched image with profile `none`.

## Current per-job parameter profiles

The current source branch is `codex/stress-job-parameters`. Use matching Control
and workload images from the [paired-image deployment guide](../../../../docker/STRESS_JOB_RELEASE_GUIDE.md).
The current A2 publication and verification scope is recorded there; an A2
result does not establish A3 hardware support or complete model validation.

| Target | Explicit profile | Patches, in application order |
| --- | --- | --- |
| A2 | `a2-upstream-model-sdc-memory-job-v1` | `portable-runtime-v1.patch`, `a2-upstream-base.patch`, `model-sdc-v1.patch`, `model-memory-v1.patch`, `job-parameters-v1.patch` |
| A3 | `portable-model-sdc-memory-job-v1` | `portable-runtime-v1.patch`, `model-sdc-v1.patch`, `model-memory-v1.patch`, `job-parameters-v1.patch` |

A2 uses the unchanged complete upstream base configuration. A3 does not apply
the A2 generation/config selector or sizing patch; its complete configuration
is preserved. Neither profile invents an A2 QuantMatmul strategy or silently
replaces unsupported dtypes. The installed Python/JSON inventory is checked
against the patched source, without pruning operators, models or configurations.

### Patch responsibilities

- `portable-runtime-v1.patch`: optional dtype registration, package-qualified
  imports, sysfs/cgroup CPU topology, evidence-based single-device mapping,
  named chip-case overrides, operator catalog and bounded worker lifecycle.
- `a2-upstream-base.patch`: accepts A2 while preserving upstream dimensions,
  iterations, dtypes, patterns and worker data pool. It does not create an
  `a2_config.json` or modify the base/A3 JSON files.
- `model-sdc-v1.patch`: writable reference/trace directories for complete
  upstream models, while keeping the image root read-only.
- `model-memory-v1.patch`: opt-in `CATMONITOR_MODEL_MEMORY_MODE=checkpointed`,
  using non-reentrant activation checkpointing with RNG preservation. Default
  `standard` is unchanged; dimensions, batch, dtype and step counts are not
  reduced. Missing checkpoint APIs fail rather than falling back. The mode is
  part of reference identity.
- `job-parameters-v1.patch`: validated frozen per-job configuration and explicit
  SDC tolerances; no shared deployment configuration is rewritten for a job.

The portable profile separates stream synchronization timeout from the whole-job
budget. CATMonitor supplies `CATMONITOR_NPU_JOB_TIMEOUT_SECONDS` from the validated
request; standalone upstream invocation defaults to 3600 seconds. Worker readiness
is bounded by 60 seconds and the remaining job budget. Startup failure, early exit
and deadline expiry propagate failures and reap started processes. No additional
CLI `exec_count` is invented.

Background stream-synchronization exceptions are rethrown to the calling worker;
a CANN error must not be interpreted as successful synchronization. The workload
plugin places each job's cwd and HOME in bounded writable tmpfs, allowing relative
`kernel_meta` JIT writes with a read-only image root. Job cleanup removes temporary
files without deleting persisted results.

## Earlier explicit compatibility profiles

These profiles remain available for reproducing older builds. They are not the
current per-job/model deployment recipe and must not be stacked arbitrarily.

| Profile | Ordered patches | Scope |
| --- | --- | --- |
| `none` | none | Pristine bundled source; no portable fixes |
| `a2-cann83` | `a2-cann83.patch` | Historical Ascend910B4 / CANN 8.3.RC2 / torch and torch_npu 2.8 combination; explicit logical count when PCI is hidden, 1 GiB pool and FP16 4096×4096/20-iteration matmul |
| `portable-runtime-v1` | `portable-runtime-v1.patch` | Portable runtime fixes without A2-specific changes |
| `a2-portable-runtime-v1` | `portable-runtime-v1.patch`, `a2-portable-sizing.patch` | A2 smoke configuration: separate `a2_config.json`, 1 GiB pool and FP16 4096×4096/20-iteration matmul; base/A3 configs unchanged |
| `a2-upstream-base-v1` | `portable-runtime-v1.patch`, `a2-upstream-base.patch` | Complete upstream A2 base configuration, without model/job patches |

Do not combine `a2-cann83.patch` with the portable chain, or combine
`a2-portable-sizing.patch` with `a2-upstream-base.patch`. Existing compatibility
profiles do not imply certification for arbitrary CANN/torch stacks.

## Build and validation references

- [Developer guide](../../../../docker/DEVELOPER_GUIDE.md): build inputs,
  ordered `--patch` arguments, manifests, networks and offline boundaries.
- [Stress user guide](../../../../features/stress/STRESS_USER_GUIDE.md): device
  authorization, per-job parameters, Controller and result semantics.
- [Stress test guide](../../../../features/stress/STRESS_TEST_GUIDE.md): synthetic
  regressions and hardware acceptance boundaries.
- [Model SDC guide](../../../../docker/NPU_MODEL_SDC_GUIDE.md): reference matching,
  memory modes and model results. Import/build checks are not hardware certification.
