# Ascend NPU Burn compatibility patches

Keep CATMonitor-specific compatibility changes here instead of editing
`third_party/ascend_npu_burn/source`.

The initial bundled A3 profile is `none` and applies no patch.

`a2-cann83.patch` is the named compatibility profile for the verified Ascend
910B4 (A2), CANN 8.3.RC2, torch 2.8 and torch_npu 2.8 environment. It accepts
the A2 generation, handles optional newer dtypes, fixes output-directory
validation, requires an explicit logical device count when PCI topology is
hidden, bounds the data pool to 1 GiB, and narrows the matmul profile to FP16
4096x4096 for 20 iterations.

The A2 patch is never applied implicitly. Builds must select
`--compat-profile a2-cann83` and pass the patch explicitly. Pristine A3 builds
may use `none`; this does not include the portable fixes below.

`portable-runtime-v1.patch` is a public-source compatibility candidate: optional
dtype registration, package-qualified imports, sysfs/cgroup CPU topology,
kernel-proven single-device mapping and bounded worker lifecycle. Select
`--compat-profile portable-runtime-v1 --patch <path-to-this-patch>` explicitly.
It is based on the pristine bundled tree, NOT on `a2-cann83.patch`; do not stack
these profiles. It preserves each upstream case's dimensions, merges chip case
overrides by name rather than replacing the complete base case list, and adds
a capability-checked operator catalog. It does not certify A2/A3/A5 or any
CANN/torch stack. New-image hardware acceptance is NOT_RUN. See the
[audit and gates](../../../../docker/NPU_COMPAT_REVIEW.md).

For an **A2 compatibility candidate**, use `--compat-profile a2-portable-runtime-v1`
and, in this exact order, `--patch portable-runtime-v1.patch` followed by
`--patch a2-portable-sizing.patch`. The second patch only adds the A2 generation,
1 GiB A2 worker pool and an independent `a2_config.json` with the FP16
4096x4096/20-iteration matmul profile. Base and A3 configs remain byte-identical
to the bundled tree. Other cases keep their upstream dimensions. It does not
restore the old topology fallback or invent an A2 quant_matmul strategy.
Do not combine either with `a2-cann83.patch`.
For full upstream base-configuration validation on A2, use
`--compat-profile a2-upstream-base-v1` with `portable-runtime-v1.patch` then
`a2-upstream-base.patch`, **instead of** the sizing patch. This alternative
only admits the A2 generation and uses the unchanged `base_config.json` for
both admission and execution. It does not create `a2_config.json`, reduce
the data pool, iteration counts, dtypes, dimensions or patterns. A3 still
uses its complete original override. A2 QuantMatmul remains unsupported;
model SDC reference requirements still apply. Full parameters are not a
claim of hardware certification.
Both ordered-patch synthetic suites pass; the newly built A2 image and real
workload remain NOT_RUN. This candidate is not a replacement hardware
certification for the historical A2 profile.

The installed Python/JSON tree is checked against the patched source inventory;
no operator, model or configuration directories are pruned. Per-Job device and
operator selection, capability limits and the pending hardware matrix are in the
[operator guide](../../../../docker/NPU_OPERATOR_GUIDE.md).

The portable profile separates the stream synchronization `--timeout` from
the whole-job budget. CATMonitor supplies `CATMONITOR_NPU_JOB_TIMEOUT_SECONDS`
from the validated Controller request; standalone upstream invocation defaults
to 3600 seconds. Worker readiness is bounded by 60 seconds and the remaining
job budget. Startup failure, early exit and deadline expiry propagate failures
and reap already-started processes; no additional CLI `exec_count` is invented.

The portable patch also rethrows background stream-synchronization exceptions
in the calling worker. A thread that exits with a CANN compilation error must
not be interpreted as a successful synchronization. Synthetic tests cover
success, the original exception, and timeout. CATMonitor's NPU workload plugin
sets per-job cwd as well as HOME to its bounded writable tmpfs, so CANN's relative
`kernel_meta` JIT writes work with a read-only image root. Temporary files are
removed by the existing job cleanup; base/A3 configurations remain unchanged.

The image builder accepts repeatable `--patch` paths for development and
compatibility validation. Every applied patch path and SHA-256 is recorded in
the build manifest, and patches are applied only to an isolated source copy.

`model-sdc-v1.patch` supplies explicit writable reference/trace directories for
the complete upstream models, without making the image root writable.
`model-memory-v1.patch` adds opt-in `CATMONITOR_MODEL_MEMORY_MODE=checkpointed`:
non-reentrant activation checkpointing with RNG preservation. The default is
`standard`; no dimensions, batch, dtype or step counts are reduced. An absent
Transformers checkpoint API is an error, not a fallback. This policy is bound
to reference identity, and its full A2/A3 hardware validation is PENDING.
For A3, apply portable-runtime-v1, model-sdc-v1, model-memory-v1 in that order,
with an explicit `portable-model-sdc-memory-v1` profile; do not apply the A2
config-selector or sizing patches. See the [A3 migration guide](../../../../docker/NPU_A3_MIGRATION_GUIDE.md).
