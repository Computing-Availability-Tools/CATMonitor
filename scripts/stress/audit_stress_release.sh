#!/usr/bin/env bash
# Validate the repository-owned stress release evidence. This is a mechanical
# completeness check, not legal advice and not a substitute for an SBOM of a
# deployment bundle that contains administrator-supplied benchmark binaries.

set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/../.." && pwd -P)
CPU_MANIFEST=
NPU_MANIFEST=
REQUIRE_RUNTIME_MANIFESTS=false

usage() {
    cat <<'EOF'
Usage: audit_stress_release.sh [OPTIONS]

  --cpu-manifest PATH          CPU build manifest included in a deployment bundle
  --npu-manifest PATH          NPU image manifest included in a deployment bundle
  --require-runtime-manifests  Fail unless both manifests are supplied
  -h, --help                   Show this help

With no manifest arguments, audit only repository-distributed material.
Supplying both manifests extends the check to deployment provenance, but does
not generate or certify third-party binary license notices or an SBOM.
EOF
}

die() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
require_value() { [ "$#" -ge 2 ] && [ -n "$2" ] || die "$1 requires a value"; }

while [ "$#" -gt 0 ]; do
    case "$1" in
        --cpu-manifest) require_value "$@"; CPU_MANIFEST=$2; shift 2 ;;
        --npu-manifest) require_value "$@"; NPU_MANIFEST=$2; shift 2 ;;
        --require-runtime-manifests) REQUIRE_RUNTIME_MANIFESTS=true; shift ;;
        -h|--help) usage; exit 0 ;;
        *) die "unknown argument: $1" ;;
    esac
done

require_file() { [ -f "$REPO_ROOT/$1" ] || die "required repository file is missing: $1"; }
for path in \
    LICENSE \
    features/stress/THIRD_PARTY_NOTICES.md \
    features/stress/OSS_RELEASE_AUDIT.md \
    third_party/ascend_npu_burn/UPSTREAM \
    third_party/ascend_npu_burn/UPSTREAM.md \
    third_party/ascend_npu_burn/SOURCE_SHA256SUMS \
    third_party/ascend_npu_burn/source/LICENSE.md \
    third_party/ascend_npu_burn/source/docs/LICENSE \
    docker/stress/cpu/Dockerfile \
    docker/stress/cpu/entrypoint.sh \
    scripts/stress/build_cpu_runner_image.sh \
    docker/stress/npu/runtime-packages.txt; do
    require_file "$path"
done

grep -Fxq 'license=MulanPSL-2.0' "$REPO_ROOT/third_party/ascend_npu_burn/UPSTREAM" ||
    die "bundled NPU Burn license identity is missing from UPSTREAM"
grep -Fq 'Mulan PSL v2' "$REPO_ROOT/features/stress/THIRD_PARTY_NOTICES.md" ||
    die "stress third-party notice does not identify the bundled NPU Burn license"
grep -Fq 'COPY --from=npuburn_builder /opt/catmonitor/npuburn-source/LICENSE.md' \
    "$REPO_ROOT/docker/stress/npu/Dockerfile" ||
    die "NPU Burn runtime image no longer copies its license"

grep -Fq 'build_cpu_benchmarks.sh' "$REPO_ROOT/docker/stress/cpu/Dockerfile" ||
    die "CPU runner image no longer builds benchmarks in its controlled builder stage"
grep -Fq 'network_mode: none' "$REPO_ROOT/docker/docker-compose.stress.yml" ||
    die "CPU runner deployment no longer disables networking"
if grep -Fq '/var/run/docker.sock' "$REPO_ROOT/docker/docker-compose.stress.yml"; then
    die "CPU runner deployment must not receive the Docker socket"
fi

(
    cd "$REPO_ROOT/third_party/ascend_npu_burn/source"
    sha256sum --check --strict ../SOURCE_SHA256SUMS >/dev/null
) || die "bundled NPU Burn source checksum verification failed"

package_list="$REPO_ROOT/docker/stress/npu/runtime-packages.txt"
grep -Fxq pciutils "$package_list" || die "runtime package list must contain pciutils"
if grep -nE '^[[:space:]]*$|^[[:space:]]|[[:space:]]$' "$package_list" >/dev/null; then
    die "runtime package list must contain non-empty, unpadded package names"
fi
if [ "$(sort "$package_list" | uniq -d | wc -l)" -ne 0 ]; then
    die "runtime package list contains duplicate names"
fi

validate_manifest() {
    local label=$1 path=$2
    [ -f "$path" ] || die "$label manifest is unavailable: $path"
    # Numeric JSON values are canonical. Quoted positive integers remain
    # accepted so deployments created by earlier CATMonitor releases and the
    # current NPU image manifest schema can still be audited.
    grep -Eq '"schema_version"[[:space:]]*:[[:space:]]*("[1-9][0-9]*"|[1-9][0-9]*)[[:space:]]*[,}]' "$path" ||
        die "$label manifest does not declare a positive schema_version: $path"
    sha256sum -- "$path" | awk -v label="$label" '{print "PASS: " label " manifest sha256=" $1}'
}

if [ "$REQUIRE_RUNTIME_MANIFESTS" = true ] && { [ -z "$CPU_MANIFEST" ] || [ -z "$NPU_MANIFEST" ]; }; then
    die "--require-runtime-manifests requires --cpu-manifest and --npu-manifest"
fi
[ -z "$CPU_MANIFEST" ] || validate_manifest CPU "$CPU_MANIFEST"
[ -z "$NPU_MANIFEST" ] || validate_manifest NPU "$NPU_MANIFEST"

printf 'PASS: repository stress license, provenance, checksum and runtime-package evidence\n'
if [ -z "$CPU_MANIFEST" ] || [ -z "$NPU_MANIFEST" ]; then
    printf 'INFO: deployment manifests were not supplied; deployment SBOM/license closure was not evaluated\n'
fi
