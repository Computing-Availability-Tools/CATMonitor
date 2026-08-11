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
`--compat-profile a2-cann83` and pass the patch explicitly. A3 continues to use
`--compat-profile none` unless a separately reviewed failure justifies its own
profile.

The image builder accepts repeatable `--patch` paths for development and
compatibility validation. Every applied patch path and SHA-256 is recorded in
the build manifest, and patches are applied only to an isolated source copy.
