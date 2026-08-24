#!/usr/bin/env bash

set -euo pipefail

REPO_ROOT=$(cd "$(dirname "$0")/../../.." && pwd -P)
BUILD_SCRIPT="$REPO_ROOT/scripts/stress/build_cpu_runner_image.sh"
TEST_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/catmonitor-cpu-runner-image-test.XXXXXXXX")

cleanup() {
    case "$TEST_ROOT" in "${TMPDIR:-/tmp}"/catmonitor-cpu-runner-image-test.*) rm -rf -- "$TEST_ROOT" ;; esac
}
trap cleanup EXIT HUP INT TERM

fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }

install -d -m 0755 "$TEST_ROOT/bin" "$TEST_ROOT/inputs"
printf '/* STREAM fixture */\n' >"$TEST_ROOT/inputs/stream.c"
printf 'HPL archive fixture\n' >"$TEST_ROOT/inputs/hpl.tar.gz"
printf 'HPL.dat fixture\n' >"$TEST_ROOT/inputs/HPL.dat"
printf 'HPCG archive fixture\n' >"$TEST_ROOT/inputs/hpcg.tar.gz"
printf '32 32 32\n60\n' >"$TEST_ROOT/inputs/hpcg.dat"

cat >"$TEST_ROOT/bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
: "${FAKE_DOCKER_STATE:?}"
: "${FAKE_DOCKER_LOG:?}"
printf '%q ' "$@" >>"$FAKE_DOCKER_LOG"
printf '\n' >>"$FAKE_DOCKER_LOG"
if [ "${1-}" = image ] && [ "${2-}" = inspect ]; then
    if [ "${3-}" = --format ]; then
        [ -f "$FAKE_DOCKER_STATE" ] || exit 1
        printf 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n'
        exit 0
    fi
    [ -f "$FAKE_DOCKER_STATE" ]
    exit
fi
if [ "${1-}" = build ]; then
    context=${!#}
    test -f "$context/inputs/stream.c"
    test -f "$context/inputs/hpl.tar.gz"
    test -f "$context/inputs/HPL.dat"
    test -f "$context/inputs/hpcg.tar.gz"
    test -f "$context/inputs/hpcg.dat"
    test -f "$context/features/stress/runnerapi/server_linux.go"
    test -f "$context/features/stress/cmd/cpu-runner/main_linux.go"
    test -f "$context/scripts/stress/build_cpu_benchmarks.sh"
    test -f "$context/docker/stress/cpu/entrypoint.sh"
    grep -Fq -- '--stream-smoke-numa-policy "$STREAM_SMOKE_NUMA_POLICY"' \
        "$context/docker/stress/cpu/Dockerfile"
    grep -Fq 'libmpich-dev' "$context/docker/stress/cpu/Dockerfile"
    [ "$(grep -Fc 'ARG DEBIAN_MIRROR=""' "$context/docker/stress/cpu/Dockerfile")" -eq 2 ]
    grep -Fq '${root}/debian-security' "$context/docker/stress/cpu/Dockerfile"
    touch "$FAKE_DOCKER_STATE"
    exit 0
fi
exit 2
EOF
chmod 0755 "$TEST_ROOT/bin/docker"
export FAKE_DOCKER_STATE="$TEST_ROOT/image.exists"
export FAKE_DOCKER_LOG="$TEST_ROOT/docker.log"
export HTTPS_PROXY=https://user:secret@proxy.invalid:8443

grep -Fq -- '--debian-mirror URL' <(bash "$BUILD_SCRIPT" --help) ||
    fail '--help does not describe --debian-mirror'

MANIFEST="$TEST_ROOT/output/cpu-runner-image.json"
bash "$BUILD_SCRIPT" \
    --image catmonitor/stress-cpu:test \
    --docker-bin "$TEST_ROOT/bin/docker" \
    --stream-src "$TEST_ROOT/inputs/stream.c" \
    --hpl-src "$TEST_ROOT/inputs/hpl.tar.gz" \
    --hpl-dat "$TEST_ROOT/inputs/HPL.dat" \
    --hpcg-src "$TEST_ROOT/inputs/hpcg.tar.gz" \
    --hpcg-dat "$TEST_ROOT/inputs/hpcg.dat" \
    --build-root "$TEST_ROOT/build" \
    --manifest "$MANIFEST" \
    --debian-mirror https://mirror.example.invalid/ \
    --jobs 4 \
    --stream-array-size 4096 \
    --stream-ntimes 3

test -s "$MANIFEST" || fail 'CPU runner image manifest was not generated'
if command -v python3 >/dev/null 2>&1; then python3 -m json.tool "$MANIFEST" >/dev/null; fi
grep -Fq '"schema_version":1' "$MANIFEST" || fail 'manifest schema is not numeric'
grep -Fq '"feature":"stress_cpu_runner"' "$MANIFEST" || fail 'manifest feature is missing'
grep -Fq '"image":"catmonitor/stress-cpu:test"' "$MANIFEST" || fail 'manifest image is missing'
grep -Fq '"debian_mirror":"https://mirror.example.invalid"' "$MANIFEST" ||
    fail 'manifest Debian mirror provenance is missing or not normalized'
grep -Fq '"jobs":4' "$MANIFEST" || fail 'manifest build profile is missing'
grep -Fq -- '--network default' "$FAKE_DOCKER_LOG" || fail 'build network was not forwarded'
grep -Fq -- '--build-arg HTTPS_PROXY' "$FAKE_DOCKER_LOG" || fail 'proxy variable name was not forwarded'
grep -Fq -- '--build-arg DEBIAN_MIRROR=https://mirror.example.invalid' "$FAKE_DOCKER_LOG" ||
    fail 'Debian mirror build argument was not forwarded'
if grep -Fq 'user:secret' "$FAKE_DOCKER_LOG"; then
    fail 'proxy credential was expanded into the Docker command log'
fi
find "$TEST_ROOT/build" -maxdepth 1 -type d -name 'context.*' -print -quit | grep -q . &&
    fail 'isolated Docker context was not cleaned'

if bash "$BUILD_SCRIPT" \
    --image catmonitor/stress-cpu:test \
    --docker-bin "$TEST_ROOT/bin/docker" \
    --stream-src "$TEST_ROOT/inputs/stream.c" \
    --hpl-src "$TEST_ROOT/inputs/hpl.tar.gz" \
    --hpl-dat "$TEST_ROOT/inputs/HPL.dat" \
    --hpcg-src "$TEST_ROOT/inputs/hpcg.tar.gz" \
    --hpcg-dat "$TEST_ROOT/inputs/hpcg.dat" \
    --build-root "$TEST_ROOT/build" \
    --manifest "$MANIFEST" >/dev/null 2>&1; then
    fail 'existing image was replaced without --force'
fi

rm -f "$FAKE_DOCKER_STATE"
: >"$FAKE_DOCKER_LOG"
DEFAULT_MANIFEST="$TEST_ROOT/output/cpu-runner-image-default.json"
bash "$BUILD_SCRIPT" \
    --image catmonitor/stress-cpu:default-test \
    --docker-bin "$TEST_ROOT/bin/docker" \
    --stream-src "$TEST_ROOT/inputs/stream.c" \
    --hpl-src "$TEST_ROOT/inputs/hpl.tar.gz" \
    --hpl-dat "$TEST_ROOT/inputs/HPL.dat" \
    --hpcg-src "$TEST_ROOT/inputs/hpcg.tar.gz" \
    --hpcg-dat "$TEST_ROOT/inputs/hpcg.dat" \
    --build-root "$TEST_ROOT/build-default" \
    --manifest "$DEFAULT_MANIFEST"
grep -Fq '"debian_mirror":null' "$DEFAULT_MANIFEST" ||
    fail 'default manifest must record a null Debian mirror override'
if grep -Fq 'DEBIAN_MIRROR=' "$FAKE_DOCKER_LOG"; then
    fail 'default build unexpectedly forwarded a Debian mirror build argument'
fi

for invalid_mirror in \
    ftp://mirror.example.invalid \
    https://user:password@mirror.example.invalid; do
    if bash "$BUILD_SCRIPT" \
        --image catmonitor/stress-cpu:invalid-test \
        --docker-bin "$TEST_ROOT/bin/docker" \
        --stream-src "$TEST_ROOT/inputs/stream.c" \
        --hpl-src "$TEST_ROOT/inputs/hpl.tar.gz" \
        --hpl-dat "$TEST_ROOT/inputs/HPL.dat" \
        --hpcg-src "$TEST_ROOT/inputs/hpcg.tar.gz" \
        --hpcg-dat "$TEST_ROOT/inputs/hpcg.dat" \
        --build-root "$TEST_ROOT/build-invalid" \
        --debian-mirror "$invalid_mirror" >/dev/null 2>&1; then
        fail "invalid Debian mirror was accepted: $invalid_mirror"
    fi
done

for forbidden_mirror in \
    "repo"."huaweicloud.com" \
    "mirrors"."tuna.tsinghua.edu.cn"; do
    if grep -R -F --exclude=build_cpu_runner_image_test.sh "$forbidden_mirror" \
        "$REPO_ROOT/scripts" "$REPO_ROOT/docker" "$REPO_ROOT/features" >/dev/null; then
        fail "repository hard-codes a site-specific Debian mirror: $forbidden_mirror"
    fi
done

printf 'PASS: CPU runner image build context, manifest and replacement boundary\n'
