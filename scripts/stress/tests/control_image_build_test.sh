#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT=$(cd "$(dirname "$0")/../../.." && pwd -P)
TMP=$(mktemp -d)
trap 'rm -rf -- "$TMP"' EXIT HUP INT TERM
FAKE_DOCKER="$TMP/docker"
FAKE_LOG="$TMP/docker.log"

fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }

cat >"$FAKE_DOCKER" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
: "${FAKE_DOCKER_LOG:?}"
printf '<%s>\n' "$@" >>"$FAKE_DOCKER_LOG"
EOF
chmod 0755 "$FAKE_DOCKER"

run_build() {
    : >"$FAKE_LOG"
    FAKE_DOCKER_LOG="$FAKE_LOG" \
        CATMONITOR_DOCKER_BIN="$FAKE_DOCKER" \
        "$@"
}

run_build env \
    CATMONITOR_ALPINE_MIRROR=https://mirror.example.invalid/alpine \
    bash "$REPO_ROOT/docker/build.sh" generic >/dev/null
grep -Fxq '<--build-arg>' "$FAKE_LOG" || fail 'generic build did not pass a build argument'
grep -Fxq '<ALPINE_MIRROR=https://mirror.example.invalid/alpine>' "$FAKE_LOG" ||
    fail 'generic build did not pass the configured Alpine mirror'
if grep -Fq 'DEBIAN_MIRROR=' "$FAKE_LOG"; then
    fail 'generic build passed an unrelated Debian mirror'
fi

run_build env \
    CATMONITOR_DEBIAN_MIRROR=https://mirror.example.invalid \
    bash "$REPO_ROOT/docker/build.sh" gpu >/dev/null
grep -Fxq '<DEBIAN_MIRROR=https://mirror.example.invalid>' "$FAKE_LOG" ||
    fail 'GPU build did not pass the configured Debian mirror'
if grep -Fq 'ALPINE_MIRROR=' "$FAKE_LOG"; then
    fail 'GPU build passed an unrelated Alpine mirror'
fi

run_build bash "$REPO_ROOT/docker/build.sh" generic >/dev/null
if grep -Eq '(ALPINE|DEBIAN)_MIRROR=' "$FAKE_LOG"; then
    fail 'default build unexpectedly passed a package mirror'
fi

: >"$FAKE_LOG"
if FAKE_DOCKER_LOG="$FAKE_LOG" CATMONITOR_DOCKER_BIN="$FAKE_DOCKER" \
    CATMONITOR_ALPINE_MIRROR='https://mirror.invalid/alpine;bad' \
    bash "$REPO_ROOT/docker/build.sh" generic >/dev/null 2>&1; then
    fail 'unsafe Alpine mirror was accepted'
fi
[ ! -s "$FAKE_LOG" ] || fail 'Docker ran after invalid Alpine mirror input'

: >"$FAKE_LOG"
if FAKE_DOCKER_LOG="$FAKE_LOG" CATMONITOR_DOCKER_BIN="$FAKE_DOCKER" \
    CATMONITOR_DEBIAN_MIRROR='https://mirror.invalid/debian' \
    bash "$REPO_ROOT/docker/build.sh" gpu >/dev/null 2>&1; then
    fail 'Debian mirror with a path was accepted'
fi
[ ! -s "$FAKE_LOG" ] || fail 'Docker ran after invalid Debian mirror input'

for file in docker/Dockerfile.gpu docker/Dockerfile.npu; do
    grep -Fq 'ARG DEBIAN_MIRROR=""' "$REPO_ROOT/$file" || fail "$file lacks DEBIAN_MIRROR"
    grep -Fq '${root}/debian-security' "$REPO_ROOT/$file" || fail "$file lacks security mirror routing"
    grep -Fq '/etc/apt/sources.list.d/*.sources' "$REPO_ROOT/$file" || fail "$file lacks deb822 support"
done
grep -Fq 'ARG ALPINE_MIRROR=""' "$REPO_ROOT/docker/Dockerfile.generic" ||
    fail 'generic Dockerfile lacks ALPINE_MIRROR'
grep -Fq '/etc/apk/repositories' "$REPO_ROOT/docker/Dockerfile.generic" ||
    fail 'generic Dockerfile does not update Alpine repositories'

if grep -R -F --exclude=control_image_build_test.sh 'mirrors.aliyun.com' \
    "$REPO_ROOT/docker/build.sh" \
    "$REPO_ROOT/docker/Dockerfile.generic" \
    "$REPO_ROOT/docker/Dockerfile.gpu" \
    "$REPO_ROOT/docker/Dockerfile.npu" >/dev/null; then
    fail 'control image build hard-codes a site-specific package mirror'
fi

printf 'PASS: optional control-image package mirror contract\n'
