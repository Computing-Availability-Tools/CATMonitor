#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
BUILD_SCRIPT=$(cd -- "$SCRIPT_DIR/.." && pwd -P)/build_cpu_benchmarks.sh
AUDIT_SCRIPT=$(cd -- "$SCRIPT_DIR/.." && pwd -P)/audit_stress_release.sh
TEST_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/catmonitor-stress-build-test.XXXXXXXX")

cleanup() {
    case "$TEST_ROOT" in */catmonitor-stress-build-test.*) rm -rf -- "$TEST_ROOT" ;; esac
}
trap cleanup EXIT HUP INT TERM

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

assert_file() {
    [ -f "$1" ] || fail "missing file: $1"
}

assert_executable() {
    [ -x "$1" ] || fail "missing executable: $1"
}

assert_contains() {
    grep -Fq -- "$2" "$1" || fail "$1 does not contain: $2"
}

assert_fails() {
    local log_file=$1
    shift
    if "$@" >"$log_file" 2>&1; then
        fail "command unexpectedly succeeded: $*"
    fi
}

install -d -m 0755 \
    "$TEST_ROOT/tools" \
    "$TEST_ROOT/sources/stream in another tree" \
    "$TEST_ROOT/archives" \
    "$TEST_ROOT/configs/hpl" \
    "$TEST_ROOT/configs/hpcg" \
    "$TEST_ROOT/openblas/include" \
    "$TEST_ROOT/openblas/lib"

cat >"$TEST_ROOT/tools/fake-cc" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [ "${1-}" = --version ]; then
    echo 'fake gcc 10.2.0'
    exit 0
fi
output=
while [ "$#" -gt 0 ]; do
    if [ "$1" = -o ]; then
        output=$2
        shift 2
    else
        shift
    fi
done
[ -n "$output" ]
cat >"$output" <<'PROGRAM'
#!/usr/bin/env bash
echo 'Copy: 100000.0'
echo 'Scale: 99000.0'
echo 'Add: 98000.0'
echo 'Triad: 97000.0'
echo 'Solution Validates: avg error less than epsilon on all three arrays'
PROGRAM
chmod +x "$output"
EOF

cat >"$TEST_ROOT/tools/fake-cxx" <<'EOF'
#!/usr/bin/env bash
if [ "${1-}" = --version ]; then echo 'fake g++ 10.2.0'; exit 0; fi
exit 0
EOF

cat >"$TEST_ROOT/tools/fake-mpicc" <<'EOF'
#!/usr/bin/env bash
case "${1-}" in
    -show) echo 'gcc -I/fake/mpich/include -L/fake/mpich/lib -lmpi' ;;
    --version) echo 'fake mpicc 4.1.3' ;;
    *) exit 0 ;;
esac
EOF

cat >"$TEST_ROOT/tools/fake-mpicxx" <<'EOF'
#!/usr/bin/env bash
case "${1-}" in
    -show) echo 'g++ -I/fake/mpich/include -L/fake/mpich/lib -lmpicxx -lmpi' ;;
    --version) echo 'fake mpicxx 4.1.3' ;;
    *) exit 0 ;;
esac
EOF

cat >"$TEST_ROOT/tools/fake-mpirun" <<'EOF'
#!/usr/bin/env bash
if [ "${1-}" = --version ]; then echo 'HYDRA build details: Version 4.1.3'; exit 0; fi
exit 0
EOF

cat >"$TEST_ROOT/tools/fake-openmpi-run" <<'EOF'
#!/usr/bin/env bash
if [ "${1-}" = --version ]; then echo 'mpirun (Open MPI) 4.1.5'; exit 0; fi
exit 0
EOF

cat >"$TEST_ROOT/tools/numactl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[ "${1-}" = --interleave=all ]
shift
exec "$@"
EOF

chmod +x "$TEST_ROOT/tools/"*
export PATH="$TEST_ROOT/tools:$PATH"

printf '/* fake STREAM source fixture */\n' >"$TEST_ROOT/sources/stream in another tree/stream.c"
printf 'HPL input owned by administrator\n' >"$TEST_ROOT/configs/hpl/HPL.dat"
printf '32 32 32\n60\n' >"$TEST_ROOT/configs/hpcg/hpcg.dat"
printf 'fake openblas\n' >"$TEST_ROOT/openblas/lib/libopenblas.so.0"

HPL_FIXTURE="$TEST_ROOT/hpl-fixture/hpl-2.3"
install -d -m 0755 "$HPL_FIXTURE/leaf"
printf '# fixture marker\n' >"$HPL_FIXTURE/Make.top"
cat >"$HPL_FIXTURE/Makefile" <<'EOF'
.PHONY: all install startup refresh build
all: install
install: startup refresh build

startup:
	sleep 0.2
	mkdir -p build-state/$(arch) bin/$(arch)
	touch build-state/$(arch)/startup.done
	printf 'startup\n' >> build-state/$(arch)/order.log

refresh:
	test -f build-state/$(arch)/startup.done || { echo 'startup not complete' >&2; exit 9; }
	touch build-state/$(arch)/refresh.done
	printf 'refresh\n' >> build-state/$(arch)/order.log

build:
	test -f build-state/$(arch)/refresh.done || { echo 'refresh not complete' >&2; exit 10; }
	printf 'build\n' >> build-state/$(arch)/order.log
	$(MAKE) -C leaf arch=$(arch) top=$(CURDIR)
EOF
cat >"$HPL_FIXTURE/leaf/Makefile" <<'EOF'
.PHONY: all
all:
	test -f $(top)/Make.$(arch)
	cp $(top)/fixture-xhpl $(top)/bin/$(arch)/xhpl
	order=$$(tr '\n' ' ' <$(top)/build-state/$(arch)/order.log); printf '# HPL_BUILD_ORDER=%s\n' "$$order" >> $(top)/bin/$(arch)/xhpl
	printf '# HPL_CHILD_MAKEFLAGS=%s\n' '$(MAKEFLAGS)' >> $(top)/bin/$(arch)/xhpl
	chmod +x $(top)/bin/$(arch)/xhpl
EOF
cat >"$HPL_FIXTURE/fixture-xhpl" <<'EOF'
#!/usr/bin/env bash
echo 'HPL fixture; full benchmark must not run during build'
EOF
chmod +x "$HPL_FIXTURE/fixture-xhpl"

# Model stock HPL 2.3's `install: startup refresh build` race: the old
# top-level `make -j` invocation must fail because refresh/build can run while
# startup is sleeping. The production builder must instead call the three
# independent targets in order.
assert_fails "$TEST_ROOT/hpl-parallel-install-race.log" \
    make -C "$HPL_FIXTURE" -j2 arch=RaceFixture
assert_contains "$TEST_ROOT/hpl-parallel-install-race.log" 'startup not complete'
rm -rf -- "$HPL_FIXTURE/build-state/RaceFixture" "$HPL_FIXTURE/bin/RaceFixture"

tar -czf "$TEST_ROOT/archives/hpl-source-any-name.tar.gz" -C "$TEST_ROOT/hpl-fixture" hpl-2.3

HPCG_FIXTURE="$TEST_ROOT/hpcg-fixture/hpcg-3.1"
install -d -m 0755 "$HPCG_FIXTURE/setup" "$HPCG_FIXTURE/src"
cat >"$HPCG_FIXTURE/setup/Make.MPI_GCC_OMP" <<'EOF'
CXX = g++
CXXFLAGS = $(HPCG_DEFS) -O3
LINKER = $(CXX)
LINKFLAGS = $(CXXFLAGS)
EOF
cat >"$HPCG_FIXTURE/src/ComputeResidual.cpp" <<'EOF'
#pragma omp parallel default(none) shared(local_residual, v1v, v2v)
EOF
cat >"$HPCG_FIXTURE/configure" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
config=${1:?configuration is required}
source_root=$(cd -- "$(dirname -- "$0")" && pwd -P)
[ "$PWD" != "$source_root" ]
[ -f "$source_root/setup/Make.$config" ]
cat >Makefile <<MAKEFILE
.PHONY: all
all:
	mkdir -p bin
	cp '$source_root/fixture-xhpcg' bin/xhpcg
	chmod +x bin/xhpcg
MAKEFILE
EOF
cat >"$HPCG_FIXTURE/fixture-xhpcg" <<'EOF'
#!/usr/bin/env bash
echo 'HPCG fixture; full benchmark must not run during build'
EOF
chmod +x "$HPCG_FIXTURE/configure" "$HPCG_FIXTURE/fixture-xhpcg"
tar -czf "$TEST_ROOT/archives/hpcg-source-any-name.tar.gz" -C "$TEST_ROOT/hpcg-fixture" hpcg-3.1

OUTPUT_ROOT="$TEST_ROOT/install/runtime"
BUILD_ROOT="$TEST_ROOT/work/build-parent"

bash "$BUILD_SCRIPT" \
    --stream-src "$TEST_ROOT/sources/stream in another tree/stream.c" \
    --hpl-src "$TEST_ROOT/archives/hpl-source-any-name.tar.gz" \
    --hpl-dat "$TEST_ROOT/configs/hpl/HPL.dat" \
    --hpcg-src "$TEST_ROOT/archives/hpcg-source-any-name.tar.gz" \
    --hpcg-dat "$TEST_ROOT/configs/hpcg/hpcg.dat" \
    --output-root "$OUTPUT_ROOT" \
    --build-root "$BUILD_ROOT" \
    --cc "$TEST_ROOT/tools/fake-cc" \
    --cxx "$TEST_ROOT/tools/fake-cxx" \
    --mpicc "$TEST_ROOT/tools/fake-mpicc" \
    --mpicxx "$TEST_ROOT/tools/fake-mpicxx" \
    --mpirun "$TEST_ROOT/tools/fake-mpirun" \
    --openblas-include "$TEST_ROOT/openblas/include" \
    --openblas-lib "$TEST_ROOT/openblas/lib" \
    --jobs 2 \
    --stream-array-size 1000 \
    --stream-ntimes 2

assert_executable "$OUTPUT_ROOT/stream/stream_omp"
assert_executable "$OUTPUT_ROOT/hpl/xhpl"
assert_file "$OUTPUT_ROOT/hpl/HPL.dat"
assert_executable "$OUTPUT_ROOT/hpcg/xhpcg"
assert_file "$OUTPUT_ROOT/hpcg/hpcg.dat"

MANIFEST="$TEST_ROOT/install/manifests/build-manifest.json"
assert_file "$MANIFEST"
if command -v python3 >/dev/null 2>&1; then
    python3 -m json.tool "$MANIFEST" >/dev/null
elif command -v jq >/dev/null 2>&1; then
    jq empty "$MANIFEST"
fi
assert_contains "$MANIFEST" '"schema_version":1'
assert_contains "$MANIFEST" '"stream":{'
assert_contains "$MANIFEST" '"hpl":{'
assert_contains "$MANIFEST" '"hpcg":{'
assert_contains "$MANIFEST" '"openmp_patch_applied":true'
assert_contains "$MANIFEST" '"-DSTREAM_ARRAY_SIZE=1000"'
assert_contains "$MANIFEST" 'HYDRA build details: Version 4.1.3'
assert_contains "$MANIFEST" '"implementation":"mpich"'

assert_contains "$OUTPUT_ROOT/hpl/xhpl" '# HPL_BUILD_ORDER=startup refresh build '
assert_contains "$OUTPUT_ROOT/hpl/xhpl" '# HPL_CHILD_MAKEFLAGS='
assert_contains "$OUTPUT_ROOT/hpl/xhpl" '-j2'

# A manifest freshly emitted by the CPU builder must pass the repository's
# strict deployment audit alongside the current legacy-string NPU schema.
printf '{"schema_version":"6","fixture":"npu"}\n' >"$TEST_ROOT/npu-manifest.json"
bash "$AUDIT_SCRIPT" \
    --cpu-manifest "$MANIFEST" \
    --npu-manifest "$TEST_ROOT/npu-manifest.json" \
    --require-runtime-manifests >"$TEST_ROOT/fresh-manifest-audit.log"
assert_contains "$TEST_ROOT/fresh-manifest-audit.log" 'PASS: CPU manifest sha256='
assert_contains "$TEST_ROOT/fresh-manifest-audit.log" 'PASS: NPU manifest sha256='

assert_fails "$TEST_ROOT/no-force.log" \
    bash "$BUILD_SCRIPT" \
    --only stream \
    --stream-src "$TEST_ROOT/sources/stream in another tree/stream.c" \
    --output-root "$OUTPUT_ROOT" \
    --build-root "$BUILD_ROOT" \
    --cc "$TEST_ROOT/tools/fake-cc"
assert_contains "$TEST_ROOT/no-force.log" 'use --force to replace it'

bash "$BUILD_SCRIPT" \
    --only stream \
    --stream-src "$TEST_ROOT/sources/stream in another tree/stream.c" \
    --output-root "$OUTPUT_ROOT" \
    --build-root "$BUILD_ROOT" \
    --cc "$TEST_ROOT/tools/fake-cc" \
    --stream-array-size 2000 \
    --stream-ntimes 3 \
    --force
assert_contains "$MANIFEST" '"-DSTREAM_ARRAY_SIZE=2000"'
assert_contains "$MANIFEST" '"hpl":{'
assert_contains "$MANIFEST" '"hpcg":{'

bash "$BUILD_SCRIPT" \
    --only stream,hpl \
    --skip hpl \
    --stream-src "$TEST_ROOT/sources/stream in another tree/stream.c" \
    --output-root "$TEST_ROOT/skip-install/runtime" \
    --build-root "$TEST_ROOT/skip-work/build-parent" \
    --cc "$TEST_ROOT/tools/fake-cc"
assert_executable "$TEST_ROOT/skip-install/runtime/stream/stream_omp"
[ ! -e "$TEST_ROOT/skip-install/runtime/hpl/xhpl" ] || fail '--skip hpl was ignored'

assert_fails "$TEST_ROOT/mpi-mismatch.log" \
    bash "$BUILD_SCRIPT" \
    --only hpl \
    --hpl-src "$TEST_ROOT/archives/hpl-source-any-name.tar.gz" \
    --hpl-dat "$TEST_ROOT/configs/hpl/HPL.dat" \
    --output-root "$TEST_ROOT/mpi-mismatch-install/runtime" \
    --build-root "$TEST_ROOT/mpi-mismatch-work/build-parent" \
    --mpicc "$TEST_ROOT/tools/fake-mpicc" \
    --mpirun "$TEST_ROOT/tools/fake-openmpi-run" \
    --openblas-include "$TEST_ROOT/openblas/include" \
    --openblas-lib "$TEST_ROOT/openblas/lib"
assert_contains "$TEST_ROOT/mpi-mismatch.log" 'mpicc uses mpich but mpirun uses openmpi'

install -d -m 0755 "$TEST_ROOT/symlink-install/runtime" "$TEST_ROOT/symlink-sentinel"
ln -s "$TEST_ROOT/symlink-sentinel" "$TEST_ROOT/symlink-install/runtime/stream"
assert_fails "$TEST_ROOT/symlink-target.log" \
    bash "$BUILD_SCRIPT" \
    --only stream \
    --stream-src "$TEST_ROOT/sources/stream in another tree/stream.c" \
    --output-root "$TEST_ROOT/symlink-install/runtime" \
    --build-root "$TEST_ROOT/symlink-work/build-parent" \
    --cc "$TEST_ROOT/tools/fake-cc" \
    --force
assert_contains "$TEST_ROOT/symlink-target.log" 'output directory cannot be a symbolic link'

CURRENT_HPCG="$TEST_ROOT/current-hpcg/hpcg-3.1"
install -d -m 0755 "$CURRENT_HPCG/setup" "$CURRENT_HPCG/src"
cp "$HPCG_FIXTURE/setup/Make.MPI_GCC_OMP" "$CURRENT_HPCG/setup/"
cp "$HPCG_FIXTURE/configure" "$CURRENT_HPCG/configure"
cp "$HPCG_FIXTURE/fixture-xhpcg" "$CURRENT_HPCG/fixture-xhpcg"
printf '  #pragma omp parallel shared(local_residual, v1v, v2v, n)\n' >"$CURRENT_HPCG/src/ComputeResidual.cpp"
tar -czf "$TEST_ROOT/archives/hpcg-current.tar.gz" -C "$TEST_ROOT/current-hpcg" hpcg-3.1
bash "$BUILD_SCRIPT" \
    --only hpcg \
    --hpcg-src "$TEST_ROOT/archives/hpcg-current.tar.gz" \
    --hpcg-dat "$TEST_ROOT/configs/hpcg/hpcg.dat" \
    --output-root "$TEST_ROOT/current-install/runtime" \
    --build-root "$TEST_ROOT/current-work/build-parent" \
    --mpicxx "$TEST_ROOT/tools/fake-mpicxx" \
    --mpirun "$TEST_ROOT/tools/fake-mpirun"
assert_contains "$TEST_ROOT/current-install/manifests/build-manifest.json" '"openmp_patch_applied":true'

CURRENT_FIXED_HPCG="$TEST_ROOT/current-fixed-hpcg/hpcg-3.1"
install -d -m 0755 "$CURRENT_FIXED_HPCG/setup" "$CURRENT_FIXED_HPCG/src"
cp "$HPCG_FIXTURE/setup/Make.MPI_GCC_OMP" "$CURRENT_FIXED_HPCG/setup/"
cp "$HPCG_FIXTURE/configure" "$CURRENT_FIXED_HPCG/configure"
cp "$HPCG_FIXTURE/fixture-xhpcg" "$CURRENT_FIXED_HPCG/fixture-xhpcg"
printf '  #pragma omp parallel shared(local_residual, v1v, v2v)\n' >"$CURRENT_FIXED_HPCG/src/ComputeResidual.cpp"
tar -czf "$TEST_ROOT/archives/hpcg-current-fixed.tar.gz" -C "$TEST_ROOT/current-fixed-hpcg" hpcg-3.1
bash "$BUILD_SCRIPT" \
    --only hpcg \
    --hpcg-src "$TEST_ROOT/archives/hpcg-current-fixed.tar.gz" \
    --hpcg-dat "$TEST_ROOT/configs/hpcg/hpcg.dat" \
    --output-root "$TEST_ROOT/current-fixed-install/runtime" \
    --build-root "$TEST_ROOT/current-fixed-work/build-parent" \
    --mpicxx "$TEST_ROOT/tools/fake-mpicxx" \
    --mpirun "$TEST_ROOT/tools/fake-mpirun"
assert_contains "$TEST_ROOT/current-fixed-install/manifests/build-manifest.json" '"openmp_patch_applied":false'

BAD_HPCG="$TEST_ROOT/bad-hpcg/hpcg-3.1"
install -d -m 0755 "$BAD_HPCG/setup" "$BAD_HPCG/src"
cp "$HPCG_FIXTURE/setup/Make.MPI_GCC_OMP" "$BAD_HPCG/setup/"
cp "$HPCG_FIXTURE/configure" "$BAD_HPCG/configure"
cp "$HPCG_FIXTURE/fixture-xhpcg" "$BAD_HPCG/fixture-xhpcg"
printf '#pragma omp parallel shared(v1v, v2v)\n' >"$BAD_HPCG/src/ComputeResidual.cpp"
tar -czf "$TEST_ROOT/archives/hpcg-unsupported.tar.gz" -C "$TEST_ROOT/bad-hpcg" hpcg-3.1
assert_fails "$TEST_ROOT/unsupported-hpcg.log" \
    bash "$BUILD_SCRIPT" \
    --only hpcg \
    --hpcg-src "$TEST_ROOT/archives/hpcg-unsupported.tar.gz" \
    --hpcg-dat "$TEST_ROOT/configs/hpcg/hpcg.dat" \
    --output-root "$TEST_ROOT/bad-install/runtime" \
    --build-root "$TEST_ROOT/bad-work/build-parent" \
    --mpicxx "$TEST_ROOT/tools/fake-mpicxx" \
    --mpirun "$TEST_ROOT/tools/fake-mpirun"
assert_contains "$TEST_ROOT/unsupported-hpcg.log" 'unsupported HPCG ComputeResidual.cpp OpenMP layout'

install -d -m 0755 "$TEST_ROOT/unsafe-source"
printf 'unsafe fixture\n' >"$TEST_ROOT/unsafe-source/file"
tar -czf "$TEST_ROOT/archives/unsafe.tar.gz" \
    --transform='s,^,../,' -C "$TEST_ROOT/unsafe-source" file
assert_fails "$TEST_ROOT/unsafe-archive.log" \
    bash "$BUILD_SCRIPT" \
    --only hpl \
    --hpl-src "$TEST_ROOT/archives/unsafe.tar.gz" \
    --hpl-dat "$TEST_ROOT/configs/hpl/HPL.dat" \
    --output-root "$TEST_ROOT/unsafe-install/runtime" \
    --build-root "$TEST_ROOT/unsafe-work/build-parent" \
    --mpicc "$TEST_ROOT/tools/fake-mpicc" \
    --mpirun "$TEST_ROOT/tools/fake-mpirun" \
    --openblas-include "$TEST_ROOT/openblas/include" \
    --openblas-lib "$TEST_ROOT/openblas/lib"
assert_contains "$TEST_ROOT/unsafe-archive.log" 'archive contains an unsafe path'

printf 'PASS: build_cpu_benchmarks.sh\n'
