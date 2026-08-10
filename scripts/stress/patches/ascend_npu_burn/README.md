# Ascend NPU Burn compatibility patches

Keep CATMonitor-specific compatibility changes here instead of editing
`third_party/ascend_npu_burn/source`.

The initial bundled A3 profile is `none` and applies no patch. This directory
does not yet contain an A2 or A3 compatibility patch: a patch may be added only
after its exact upstream revision, CANN/torch_npu combination, SoC scope, DFX
tests, and real-node acceptance have been recorded.

The image builder accepts repeatable `--patch` paths for development and
compatibility validation. Every applied patch path and SHA-256 is recorded in
the build manifest, and patches are applied only to an isolated source copy.
