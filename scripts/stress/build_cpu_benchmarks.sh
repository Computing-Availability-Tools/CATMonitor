#!/usr/bin/env bash
# Build and install STREAM, HPL and HPCG runtime assets for CATMonitor.
# This administrator-only build tool never edits CATMonitor configuration or
# the deployed benchmark_check.sh host adapter, and never runs a full stress job.

set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
HPL_TEMPLATE="$SCRIPT_DIR/templates/Make.HPL.CATMonitor"

OUTPUT_ROOT=/opt/catmonitor/benchmarks/runtime
BUILD_ROOT=/var/tmp/catmonitor-stress-build
STREAM_ARRAY_SIZE=80000000
STREAM_NTIMES=10
JOBS=
STREAM_SRC=
HPL_SRC=
HPL_DAT=
HPCG_SRC=
HPCG_DAT=
CC_PATH=
CXX_PATH=
MPICC_PATH=
MPICXX_PATH=
MPIRUN_PATH=
OPENBLAS_INCLUDE=
OPENBLAS_LIB=
FORCE=false

declare -A ONLY_SET=()
declare -A SKIP_SET=()

usage() {
    cat <<'EOF'
Usage: build_cpu_benchmarks.sh [OPTIONS]

Source and configuration inputs:
  --stream-src PATH          STREAM stream.c
  --hpl-src PATH             HPL source tar archive
  --hpl-dat PATH             Administrator-provided HPL.dat
  --hpcg-src PATH            HPCG source tar archive
  --hpcg-dat PATH            Administrator-provided hpcg.dat

Build and install locations:
  --output-root PATH         Runtime root (default: /opt/catmonitor/benchmarks/runtime)
  --build-root PATH          Temporary build parent (default: /var/tmp/catmonitor-stress-build)

Toolchain and libraries:
  --cc PATH                  C compiler (default: gcc from PATH)
  --cxx PATH                 C++ compiler (default: g++ from PATH)
  --mpicc PATH               MPI C wrapper (default: mpicc from PATH)
  --mpicxx PATH              MPI C++ wrapper (default: mpicxx from PATH)
  --mpirun PATH              Matching MPI launcher (default: mpirun from PATH)
  --openblas-include PATH    OpenBLAS include directory (required for HPL)
  --openblas-lib PATH        OpenBLAS library directory (required for HPL)
  --jobs N                   Parallel build jobs (default: min(nproc, 16))

STREAM compile-time settings:
  --stream-array-size N      STREAM_ARRAY_SIZE (default: 80000000)
  --stream-ntimes N          NTIMES (default: 10)

Selection and replacement:
  --only NAME[,NAME...]      Build only stream, hpl and/or hpcg; repeatable
  --skip NAME[,NAME...]      Exclude a benchmark; repeatable
  --force                    Replace existing selected runtime assets
  -h, --help                 Show this help

The script performs a short STREAM validation run. It never runs full HPL or
HPCG workloads. Runtime MPI/NUMA/thread profiles remain in benchmark_check.sh.
EOF
}

die() {
    printf 'ERROR: %s\n' "$*" >&2
    exit 1
}

log() {
    printf '==> %s\n' "$*"
}

require_value() {
    [ "$#" -ge 2 ] && [ -n "$2" ] || die "$1 requires a value"
}

add_selection() {
    local target=$1 value=$2 item
    IFS=',' read -r -a items <<<"$value"
    for item in "${items[@]}"; do
        case "$item" in
            stream|hpl|hpcg) ;;
            *) die "unsupported benchmark in $target: $item" ;;
        esac
        if [ "$target" = only ]; then
            ONLY_SET["$item"]=1
        else
            SKIP_SET["$item"]=1
        fi
    done
}

while [ "$#" -gt 0 ]; do
    case "$1" in
        --stream-src) require_value "$@"; STREAM_SRC=$2; shift 2 ;;
        --hpl-src) require_value "$@"; HPL_SRC=$2; shift 2 ;;
        --hpl-dat) require_value "$@"; HPL_DAT=$2; shift 2 ;;
        --hpcg-src) require_value "$@"; HPCG_SRC=$2; shift 2 ;;
        --hpcg-dat) require_value "$@"; HPCG_DAT=$2; shift 2 ;;
        --output-root) require_value "$@"; OUTPUT_ROOT=$2; shift 2 ;;
        --build-root) require_value "$@"; BUILD_ROOT=$2; shift 2 ;;
        --cc) require_value "$@"; CC_PATH=$2; shift 2 ;;
        --cxx) require_value "$@"; CXX_PATH=$2; shift 2 ;;
        --mpicc) require_value "$@"; MPICC_PATH=$2; shift 2 ;;
        --mpicxx) require_value "$@"; MPICXX_PATH=$2; shift 2 ;;
        --mpirun) require_value "$@"; MPIRUN_PATH=$2; shift 2 ;;
        --openblas-include) require_value "$@"; OPENBLAS_INCLUDE=$2; shift 2 ;;
        --openblas-lib) require_value "$@"; OPENBLAS_LIB=$2; shift 2 ;;
        --jobs) require_value "$@"; JOBS=$2; shift 2 ;;
        --stream-array-size) require_value "$@"; STREAM_ARRAY_SIZE=$2; shift 2 ;;
        --stream-ntimes) require_value "$@"; STREAM_NTIMES=$2; shift 2 ;;
        --only) require_value "$@"; add_selection only "$2"; shift 2 ;;
        --skip) require_value "$@"; add_selection skip "$2"; shift 2 ;;
        --force) FORCE=true; shift ;;
        -h|--help) usage; exit 0 ;;
        --) shift; [ "$#" -eq 0 ] || die "positional arguments are not supported" ;;
        *) die "unknown argument: $1" ;;
    esac
done

is_positive_integer() {
    case "$1" in ''|*[!0-9]*|0) return 1 ;; *) return 0 ;; esac
}

for value_name in STREAM_ARRAY_SIZE STREAM_NTIMES; do
    is_positive_integer "${!value_name}" || die "$value_name must be a positive integer"
done

if [ -z "$JOBS" ]; then
    JOBS=$(getconf _NPROCESSORS_ONLN 2>/dev/null || nproc 2>/dev/null || printf '1')
    is_positive_integer "$JOBS" || JOBS=1
    [ "$JOBS" -le 16 ] || JOBS=16
fi
is_positive_integer "$JOBS" || die "jobs must be a positive integer"

declare -a SELECTED=()
for benchmark in stream hpl hpcg; do
    if [ "${#ONLY_SET[@]}" -gt 0 ] && [ -z "${ONLY_SET[$benchmark]+x}" ]; then
        continue
    fi
    [ -z "${SKIP_SET[$benchmark]+x}" ] || continue
    SELECTED+=("$benchmark")
done
[ "${#SELECTED[@]}" -gt 0 ] || die "selection contains no benchmarks"

selected() {
    local wanted=$1 item
    for item in "${SELECTED[@]}"; do
        [ "$item" = "$wanted" ] && return 0
    done
    return 1
}

require_command() {
    command -v "$1" >/dev/null 2>&1 || die "required command is unavailable: $1"
}

for command_name in awk chmod cp date dirname file find grep install ldd make mktemp mv readlink rm sed sha256sum sort tar tr uname; do
    require_command "$command_name"
done

resolve_tool() {
    local configured=$1 fallback=$2 resolved
    if [ -n "$configured" ]; then
        case "$configured" in /*) ;; *) die "tool path must be absolute: $configured" ;; esac
        [ -x "$configured" ] || die "tool is not executable: $configured"
        readlink -f -- "$configured"
        return
    fi
    resolved=$(command -v "$fallback" 2>/dev/null || true)
    [ -n "$resolved" ] || return 0
    readlink -f -- "$resolved"
}

if selected stream || [ -n "$CC_PATH" ]; then CC_PATH=$(resolve_tool "$CC_PATH" gcc); fi
CXX_PATH=$(resolve_tool "$CXX_PATH" g++)
if selected hpl || [ -n "$MPICC_PATH" ]; then MPICC_PATH=$(resolve_tool "$MPICC_PATH" mpicc); fi
if selected hpcg || [ -n "$MPICXX_PATH" ]; then MPICXX_PATH=$(resolve_tool "$MPICXX_PATH" mpicxx); fi
if selected hpl || selected hpcg || [ -n "$MPIRUN_PATH" ]; then
    MPIRUN_PATH=$(resolve_tool "$MPIRUN_PATH" mpirun)
fi

require_tool_value() {
    local option=$1 value=$2
    [ -n "$value" ] || die "$option is required for the selected benchmarks and was not found in PATH"
}

selected stream && require_tool_value --cc "$CC_PATH"
if selected hpl; then
    require_tool_value --mpicc "$MPICC_PATH"
    require_tool_value --mpirun "$MPIRUN_PATH"
fi
if selected hpcg; then
    require_tool_value --mpicxx "$MPICXX_PATH"
    require_tool_value --mpirun "$MPIRUN_PATH"
fi

canonical_input_file() {
    local option=$1 value=$2
    [ -n "$value" ] || die "$option is required for the selected benchmarks"
    [ -f "$value" ] && [ -r "$value" ] || die "$option is not a readable file: $value"
    readlink -f -- "$value"
}

canonical_input_dir() {
    local option=$1 value=$2
    [ -n "$value" ] || die "$option is required for the selected benchmarks"
    [ -d "$value" ] && [ -r "$value" ] || die "$option is not a readable directory: $value"
    readlink -f -- "$value"
}

if selected stream; then STREAM_SRC=$(canonical_input_file --stream-src "$STREAM_SRC"); fi
if selected hpl; then
    HPL_SRC=$(canonical_input_file --hpl-src "$HPL_SRC")
    HPL_DAT=$(canonical_input_file --hpl-dat "$HPL_DAT")
    OPENBLAS_INCLUDE=$(canonical_input_dir --openblas-include "$OPENBLAS_INCLUDE")
    OPENBLAS_LIB=$(canonical_input_dir --openblas-lib "$OPENBLAS_LIB")
    mapfile -t openblas_candidates < <(
        find "$OPENBLAS_LIB" -maxdepth 1 \( -type f -o -type l \) \
            -name 'libopenblas.*' -print | sort
    )
    [ "${#openblas_candidates[@]}" -gt 0 ] || \
        die "no libopenblas library was found in: $OPENBLAS_LIB"
    OPENBLAS_LIBRARY=$(readlink -f -- "${openblas_candidates[0]}")
fi
if selected hpcg; then
    HPCG_SRC=$(canonical_input_file --hpcg-src "$HPCG_SRC")
    HPCG_DAT=$(canonical_input_file --hpcg-dat "$HPCG_DAT")
fi

case "$OUTPUT_ROOT" in /*) ;; *) die "--output-root must be absolute" ;; esac
case "$BUILD_ROOT" in /*) ;; *) die "--build-root must be absolute" ;; esac
case "$OUTPUT_ROOT" in /) die "--output-root cannot be the filesystem root" ;; esac
case "$BUILD_ROOT" in /|/var|/var/tmp|/tmp) die "--build-root must be a dedicated child directory" ;; esac
case "$BUILD_ROOT" in *$'\n'*|*$'\r'*|*$'\t'*|*' '*) die "--build-root cannot contain whitespace" ;; esac

OUTPUT_ROOT=$(readlink -m -- "$OUTPUT_ROOT")
BUILD_ROOT=$(readlink -m -- "$BUILD_ROOT")
MANIFEST_DIR=$(readlink -m -- "$(dirname -- "$OUTPUT_ROOT")/manifests")
MANIFEST_PATH="$MANIFEST_DIR/build-manifest.json"
FRAGMENT_DIR="$MANIFEST_DIR/.build-manifest.d"
ARCHITECTURE=$(uname -m)
case "$ARCHITECTURE" in ''|*[!A-Za-z0-9_.-]*) die "unsupported architecture name: $ARCHITECTURE" ;; esac

declare -A TARGET_FILES=(
    [stream]="$OUTPUT_ROOT/stream/stream_omp"
    [hpl]="$OUTPUT_ROOT/hpl/xhpl"
    [hpl_dat]="$OUTPUT_ROOT/hpl/HPL.dat"
    [hpcg]="$OUTPUT_ROOT/hpcg/xhpcg"
    [hpcg_dat]="$OUTPUT_ROOT/hpcg/hpcg.dat"
)

for benchmark in "${SELECTED[@]}"; do
    [ ! -L "$OUTPUT_ROOT/$benchmark" ] || \
        die "benchmark output directory cannot be a symbolic link: $OUTPUT_ROOT/$benchmark"
    case "$benchmark" in
        stream) candidates=("${TARGET_FILES[stream]}") ;;
        hpl) candidates=("${TARGET_FILES[hpl]}" "${TARGET_FILES[hpl_dat]}") ;;
        hpcg) candidates=("${TARGET_FILES[hpcg]}" "${TARGET_FILES[hpcg_dat]}") ;;
    esac
    for candidate in "${candidates[@]}"; do
        [ ! -L "$candidate" ] || die "runtime target cannot be a symbolic link: $candidate"
        if [ "$FORCE" != true ] && [ -e "$candidate" ]; then
            die "target already exists; use --force to replace it: $candidate"
        fi
    done
done

install -d -m 0755 "$BUILD_ROOT"
RUN_ROOT=$(mktemp -d "$BUILD_ROOT/catmonitor-stress-build.XXXXXXXX")
cleanup() {
    case "$RUN_ROOT" in "$BUILD_ROOT"/catmonitor-stress-build.*) rm -rf -- "$RUN_ROOT" ;; esac
}
trap cleanup EXIT HUP INT TERM
install -d -m 0755 "$RUN_ROOT/stage" "$RUN_ROOT/fragments"

json_escape() {
    local value=${1-}
    value=${value//\\/\\\\}
    value=${value//\"/\\\"}
    value=${value//$'\n'/\\n}
    value=${value//$'\r'/\\r}
    value=${value//$'\t'/\\t}
    printf '%s' "$value"
}

json_string() {
    printf '"%s"' "$(json_escape "${1-}")"
}

sha256_file() {
    sha256sum -- "$1" | awk '{print $1}'
}

command_output() {
    local output
    if ! output=$("$@" 2>&1); then
        die "command failed while recording toolchain identity: $* ($output)"
    fi
    printf '%s' "$output"
}

inspect_binary() {
    local binary=$1 ldd_rc
    [ -x "$binary" ] || die "built binary is not executable: $binary"
    INSPECT_FILE=$(file -- "$binary")
    set +e
    INSPECT_LDD=$(ldd "$binary" 2>&1)
    ldd_rc=$?
    set -e
    if grep -Eq '(^|[[:space:]])not found([[:space:]]|$)' <<<"$INSPECT_LDD"; then
        die "built binary has unresolved dynamic libraries: $binary ($INSPECT_LDD)"
    fi
    # ldd returns non-zero for valid static or script test fixtures; file(1)
    # remains authoritative for existence/type while the output is recorded.
    INSPECT_LDD_RC=$ldd_rc
}

validate_archive() {
    local archive=$1 member count=0
    while IFS= read -r member; do
        count=$((count + 1))
        case "$member" in
            /*|../*|*/../*|*/..) die "archive contains an unsafe path: $member" ;;
        esac
    done < <(tar -tf "$archive")
    [ "$count" -gt 0 ] || die "archive is empty or unreadable: $archive"
}

extract_archive() {
    local archive=$1 destination=$2
    validate_archive "$archive"
    install -d -m 0755 "$destination"
    tar --no-same-owner --no-same-permissions -xf "$archive" -C "$destination"
}

find_unique_parent() {
    local search_root=$1 marker=$2 label=$3
    mapfile -t marker_files < <(find "$search_root" -maxdepth 5 -type f -name "$marker" -print | sort)
    [ "${#marker_files[@]}" -eq 1 ] || die "$label archive must contain exactly one $marker; found ${#marker_files[@]}"
    dirname -- "${marker_files[0]}"
}

replace_template_token() {
    local file=$1 token=$2 value=$3 escaped temp
    case "$value" in *'|'*|*$'\n'*|*$'\r'*) die "unsupported value for HPL template token $token" ;; esac
    escaped=${value//\\/\\\\}
    escaped=${escaped//&/\\&}
    temp="$file.tmp"
    sed "s|@$token@|$escaped|g" "$file" >"$temp"
    mv -f -- "$temp" "$file"
}

write_stream_fragment() {
    local path=$1 binary_hash=$2 source_hash=$3
    {
        printf '{"binary":'; json_string "${TARGET_FILES[stream]}"
        printf ',"binary_sha256":'; json_string "$binary_hash"
        printf ',"source":'; json_string "$STREAM_SRC"
        printf ',"source_sha256":'; json_string "$source_hash"
        printf ',"compile_flags":["-O3","-fopenmp",'
        json_string "-DSTREAM_ARRAY_SIZE=$STREAM_ARRAY_SIZE"; printf ','
        json_string "-DNTIMES=$STREAM_NTIMES"; printf ']'
        printf ',"compiler":{"path":'; json_string "$CC_PATH"
        printf ',"version":'; json_string "$CC_VERSION"; printf '}'
        printf ',"file":'; json_string "$INSPECT_FILE"
        printf ',"ldd":'; json_string "$INSPECT_LDD"
        printf ',"ldd_exit_code":%s' "$INSPECT_LDD_RC"
        printf ',"smoke":{"status":"passed","omp_threads":4,"numa_policy":"interleave_all"}}\n'
    } >"$path"
}

write_hpl_fragment() {
    local path=$1 binary_hash=$2 source_hash=$3 dat_hash=$4 template_hash=$5
    {
        printf '{"binary":'; json_string "${TARGET_FILES[hpl]}"
        printf ',"binary_sha256":'; json_string "$binary_hash"
        printf ',"source":'; json_string "$HPL_SRC"
        printf ',"source_sha256":'; json_string "$source_hash"
        printf ',"hpl_dat":'; json_string "${TARGET_FILES[hpl_dat]}"
        printf ',"hpl_dat_sha256":'; json_string "$dat_hash"
        printf ',"make_template_sha256":'; json_string "$template_hash"
        printf ',"arch":'; json_string "$HPL_ARCH"
        printf ',"compile_flags":["-O3","-fopenmp","-funroll-loops","-DHPL_CALL_CBLAS"]'
        printf ',"mpi":{"mpicc":'; json_string "$MPICC_PATH"
        printf ',"mpicc_show":'; json_string "$MPICC_SHOW"
        printf ',"implementation":'; json_string "$MPICC_IMPLEMENTATION"
        printf ',"mpirun":'; json_string "$MPIRUN_PATH"
        printf ',"mpirun_version":'; json_string "$MPIRUN_VERSION"; printf '}'
        printf ',"openblas":{"include":'; json_string "$OPENBLAS_INCLUDE"
        printf ',"library_directory":'; json_string "$OPENBLAS_LIB"
        printf ',"library":'; json_string "$OPENBLAS_LIBRARY"
        printf ',"library_sha256":'; json_string "$(sha256_file "$OPENBLAS_LIBRARY")"; printf '}'
        printf ',"file":'; json_string "$INSPECT_FILE"
        printf ',"ldd":'; json_string "$INSPECT_LDD"
        printf ',"ldd_exit_code":%s}\n' "$INSPECT_LDD_RC"
    } >"$path"
}

write_hpcg_fragment() {
    local path=$1 binary_hash=$2 source_hash=$3 dat_hash=$4 config_hash=$5 patch_applied=$6
    {
        printf '{"binary":'; json_string "${TARGET_FILES[hpcg]}"
        printf ',"binary_sha256":'; json_string "$binary_hash"
        printf ',"source":'; json_string "$HPCG_SRC"
        printf ',"source_sha256":'; json_string "$source_hash"
        printf ',"hpcg_dat":'; json_string "${TARGET_FILES[hpcg_dat]}"
        printf ',"hpcg_dat_sha256":'; json_string "$dat_hash"
        printf ',"make_config_sha256":'; json_string "$config_hash"
        printf ',"openmp_patch_applied":%s' "$patch_applied"
        printf ',"compile_flags":["-fopenmp"]'
        printf ',"mpi":{"mpicxx":'; json_string "$MPICXX_PATH"
        printf ',"mpicxx_show":'; json_string "$MPICXX_SHOW"
        printf ',"implementation":'; json_string "$MPICXX_IMPLEMENTATION"
        printf ',"mpirun":'; json_string "$MPIRUN_PATH"
        printf ',"mpirun_version":'; json_string "$MPIRUN_VERSION"; printf '}'
        printf ',"file":'; json_string "$INSPECT_FILE"
        printf ',"ldd":'; json_string "$INSPECT_LDD"
        printf ',"ldd_exit_code":%s}\n' "$INSPECT_LDD_RC"
    } >"$path"
}

CC_VERSION=
CXX_VERSION=
MPICC_SHOW=
MPICXX_SHOW=
MPIRUN_VERSION=
[ -z "$CC_PATH" ] || CC_VERSION=$(command_output "$CC_PATH" --version)
[ -z "$CXX_PATH" ] || CXX_VERSION=$(command_output "$CXX_PATH" --version)
[ -z "$MPICC_PATH" ] || MPICC_SHOW=$(command_output "$MPICC_PATH" -show)
[ -z "$MPICXX_PATH" ] || MPICXX_SHOW=$(command_output "$MPICXX_PATH" -show)
[ -z "$MPIRUN_PATH" ] || MPIRUN_VERSION=$(command_output "$MPIRUN_PATH" --version)

detect_mpi_implementation() {
    local value
    value=$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')
    case "$value" in
        *open\ mpi*|*openmpi*|*opal_wrapper*) printf 'openmpi' ;;
        *mpich*|*hydra*) printf 'mpich' ;;
        *) printf 'unknown' ;;
    esac
}

MPICC_IMPLEMENTATION=$(detect_mpi_implementation "$MPICC_SHOW")
MPICXX_IMPLEMENTATION=$(detect_mpi_implementation "$MPICXX_SHOW")
MPIRUN_IMPLEMENTATION=$(detect_mpi_implementation "$MPIRUN_VERSION")

require_matching_mpi() {
    local wrapper_name=$1 wrapper_implementation=$2
    if [ "$wrapper_implementation" != unknown ] && \
        [ "$MPIRUN_IMPLEMENTATION" != unknown ] && \
        [ "$wrapper_implementation" != "$MPIRUN_IMPLEMENTATION" ]; then
        die "$wrapper_name uses $wrapper_implementation but mpirun uses $MPIRUN_IMPLEMENTATION"
    fi
}

selected hpl && require_matching_mpi mpicc "$MPICC_IMPLEMENTATION"
selected hpcg && require_matching_mpi mpicxx "$MPICXX_IMPLEMENTATION"

if selected stream; then
    log "building STREAM"
    install -d -m 0755 "$RUN_ROOT/stage/stream"
    STREAM_BINARY="$RUN_ROOT/stage/stream/stream_omp"
    "$CC_PATH" -O3 -fopenmp \
        "-DSTREAM_ARRAY_SIZE=$STREAM_ARRAY_SIZE" \
        "-DNTIMES=$STREAM_NTIMES" \
        "$STREAM_SRC" -o "$STREAM_BINARY"
    chmod 0755 "$STREAM_BINARY"
    inspect_binary "$STREAM_BINARY"

    NUMACTL_PATH=$(command -v numactl 2>/dev/null || true)
    [ -n "$NUMACTL_PATH" ] || die "numactl is required for the STREAM smoke check"
    if ! STREAM_SMOKE_OUTPUT=$(OMP_NUM_THREADS=4 OMP_DYNAMIC=FALSE \
        "$NUMACTL_PATH" --interleave=all "$STREAM_BINARY" 2>&1); then
        die "STREAM smoke check failed: $STREAM_SMOKE_OUTPUT"
    fi
    for marker in Copy Scale Add Triad 'Solution Validates'; do
        grep -Fq "$marker" <<<"$STREAM_SMOKE_OUTPUT" || die "STREAM smoke output is missing: $marker"
    done
    write_stream_fragment "$RUN_ROOT/fragments/stream.json" \
        "$(sha256_file "$STREAM_BINARY")" "$(sha256_file "$STREAM_SRC")"
fi

if selected hpl; then
    log "building HPL"
    [ -r "$HPL_TEMPLATE" ] || die "HPL template is unavailable: $HPL_TEMPLATE"
    extract_archive "$HPL_SRC" "$RUN_ROOT/hpl-source"
    HPL_SOURCE_ROOT=$(find_unique_parent "$RUN_ROOT/hpl-source" Make.top HPL)
    HPL_ARCH="CATMonitor_$ARCHITECTURE"
    HPL_MAKEFILE="$HPL_SOURCE_ROOT/Make.$HPL_ARCH"
    cp -- "$HPL_TEMPLATE" "$HPL_MAKEFILE"
    HPL_LAINC="-I$OPENBLAS_INCLUDE"
    HPL_LALIB="-L$OPENBLAS_LIB -Wl,-rpath,$OPENBLAS_LIB -lopenblas"
    replace_template_token "$HPL_MAKEFILE" ARCH "$HPL_ARCH"
    replace_template_token "$HPL_MAKEFILE" TOPDIR "$HPL_SOURCE_ROOT"
    replace_template_token "$HPL_MAKEFILE" CC "$MPICC_PATH"
    replace_template_token "$HPL_MAKEFILE" LINKER "$MPICC_PATH"
    replace_template_token "$HPL_MAKEFILE" LAINC "$HPL_LAINC"
    replace_template_token "$HPL_MAKEFILE" LALIB "$HPL_LALIB"
    ! grep -Eq '@[A-Z_]+@' "$HPL_MAKEFILE" || die "unresolved token remains in HPL template"
    make -C "$HPL_SOURCE_ROOT" -j"$JOBS" arch="$HPL_ARCH"
    HPL_BUILT_BINARY="$HPL_SOURCE_ROOT/bin/$HPL_ARCH/xhpl"
    inspect_binary "$HPL_BUILT_BINARY"
    install -d -m 0755 "$RUN_ROOT/stage/hpl"
    install -m 0755 "$HPL_BUILT_BINARY" "$RUN_ROOT/stage/hpl/xhpl"
    install -m 0644 "$HPL_DAT" "$RUN_ROOT/stage/hpl/HPL.dat"
    inspect_binary "$RUN_ROOT/stage/hpl/xhpl"
    write_hpl_fragment "$RUN_ROOT/fragments/hpl.json" \
        "$(sha256_file "$RUN_ROOT/stage/hpl/xhpl")" \
        "$(sha256_file "$HPL_SRC")" \
        "$(sha256_file "$HPL_DAT")" \
        "$(sha256_file "$HPL_TEMPLATE")"
fi

patch_hpcg_openmp() {
    local source=$1 legacy_unfixed legacy_fixed current_unfixed current_fixed
    local legacy_unfixed_count legacy_fixed_count current_unfixed_count current_fixed_count known_count temp
    legacy_unfixed='#pragma omp parallel default(none) shared(local_residual, v1v, v2v)'
    legacy_fixed='#pragma omp parallel default(none) shared(local_residual, v1v, v2v, n)'
    current_unfixed='#pragma omp parallel shared(local_residual, v1v, v2v, n)'
    current_fixed='#pragma omp parallel shared(local_residual, v1v, v2v)'
    legacy_unfixed_count=$(awk -v expected="$legacy_unfixed" '{ line=$0; sub(/^[[:space:]]*/, "", line); sub(/[[:space:]]*$/, "", line); if (line == expected) count++ } END { print count + 0 }' "$source")
    legacy_fixed_count=$(awk -v expected="$legacy_fixed" '{ line=$0; sub(/^[[:space:]]*/, "", line); sub(/[[:space:]]*$/, "", line); if (line == expected) count++ } END { print count + 0 }' "$source")
    current_unfixed_count=$(awk -v expected="$current_unfixed" '{ line=$0; sub(/^[[:space:]]*/, "", line); sub(/[[:space:]]*$/, "", line); if (line == expected) count++ } END { print count + 0 }' "$source")
    current_fixed_count=$(awk -v expected="$current_fixed" '{ line=$0; sub(/^[[:space:]]*/, "", line); sub(/[[:space:]]*$/, "", line); if (line == expected) count++ } END { print count + 0 }' "$source")
    known_count=$((legacy_unfixed_count + legacy_fixed_count + current_unfixed_count + current_fixed_count))
    if [ "$legacy_unfixed_count" -eq 1 ] && [ "$known_count" -eq 1 ]; then
        temp="$source.tmp"
        awk -v old="$legacy_unfixed" '{ line=$0; sub(/^[[:space:]]*/, "", line); sub(/[[:space:]]*$/, "", line); if (line == old) sub(/shared\(local_residual, v1v, v2v\)/, "shared(local_residual, v1v, v2v, n)"); print }' "$source" >"$temp"
        mv -f -- "$temp" "$source"
        HPCG_PATCH_APPLIED=true
    elif [ "$current_unfixed_count" -eq 1 ] && [ "$known_count" -eq 1 ]; then
        temp="$source.tmp"
        awk -v old="$current_unfixed" '{ line=$0; sub(/^[[:space:]]*/, "", line); sub(/[[:space:]]*$/, "", line); if (line == old) sub(/, n\)/, ")"); print }' "$source" >"$temp"
        mv -f -- "$temp" "$source"
        HPCG_PATCH_APPLIED=true
    elif [ "$known_count" -eq 1 ] && \
        { [ "$legacy_fixed_count" -eq 1 ] || [ "$current_fixed_count" -eq 1 ]; }; then
        HPCG_PATCH_APPLIED=false
    else
        die "unsupported HPCG ComputeResidual.cpp OpenMP layout"
    fi
}

prepare_hpcg_make_config() {
    local source=$1 destination=$2 temp
    temp="$destination.tmp"
    if ! awk -v cxx="$MPICXX_PATH" '
        BEGIN { cxx_count = 0; flags_count = 0 }
        /^[[:space:]]*CXX[[:space:]]*=/ {
            print "CXX = " cxx
            cxx_count++
            next
        }
        /^[[:space:]]*CXXFLAGS[[:space:]]*=/ {
            line = $0
            if (index(line, "-fopenmp") == 0) line = line " -fopenmp"
            print line
            flags_count++
            next
        }
        { print }
        END { if (cxx_count != 1 || flags_count != 1) exit 42 }
    ' "$source" >"$temp"; then
        rm -f -- "$temp"
        die "unsupported HPCG setup/Make.MPI_GCC_OMP layout"
    fi
    mv -f -- "$temp" "$destination"
}

if selected hpcg; then
    log "building HPCG"
    extract_archive "$HPCG_SRC" "$RUN_ROOT/hpcg-source"
    HPCG_SOURCE_ROOT=$(find_unique_parent "$RUN_ROOT/hpcg-source" configure HPCG)
    HPCG_BASE_CONFIG="$HPCG_SOURCE_ROOT/setup/Make.MPI_GCC_OMP"
    [ -r "$HPCG_BASE_CONFIG" ] || die "HPCG setup/Make.MPI_GCC_OMP is unavailable"
    HPCG_CONFIG_NAME=CATMonitor_MPI_OMP
    HPCG_CONFIG="$HPCG_SOURCE_ROOT/setup/Make.$HPCG_CONFIG_NAME"
    prepare_hpcg_make_config "$HPCG_BASE_CONFIG" "$HPCG_CONFIG"
    HPCG_RESIDUAL="$HPCG_SOURCE_ROOT/src/ComputeResidual.cpp"
    [ -r "$HPCG_RESIDUAL" ] || die "HPCG src/ComputeResidual.cpp is unavailable"
    patch_hpcg_openmp "$HPCG_RESIDUAL"

    HPCG_BUILD_DIR="$HPCG_SOURCE_ROOT/build_$HPCG_CONFIG_NAME"
    install -d -m 0755 "$HPCG_BUILD_DIR"
    (
        cd "$HPCG_BUILD_DIR"
        bash "$HPCG_SOURCE_ROOT/configure" "$HPCG_CONFIG_NAME"
        make -j"$JOBS"
    )
    HPCG_BUILT_BINARY="$HPCG_BUILD_DIR/bin/xhpcg"
    inspect_binary "$HPCG_BUILT_BINARY"
    install -d -m 0755 "$RUN_ROOT/stage/hpcg"
    install -m 0755 "$HPCG_BUILT_BINARY" "$RUN_ROOT/stage/hpcg/xhpcg"
    install -m 0644 "$HPCG_DAT" "$RUN_ROOT/stage/hpcg/hpcg.dat"
    inspect_binary "$RUN_ROOT/stage/hpcg/xhpcg"
    write_hpcg_fragment "$RUN_ROOT/fragments/hpcg.json" \
        "$(sha256_file "$RUN_ROOT/stage/hpcg/xhpcg")" \
        "$(sha256_file "$HPCG_SRC")" \
        "$(sha256_file "$HPCG_DAT")" \
        "$(sha256_file "$HPCG_CONFIG")" \
        "$HPCG_PATCH_APPLIED"
fi

log "installing validated runtime assets"
for benchmark in "${SELECTED[@]}"; do
    install -d -m 0755 "$OUTPUT_ROOT/$benchmark"
    case "$benchmark" in
        stream) install -m 0755 "$RUN_ROOT/stage/stream/stream_omp" "${TARGET_FILES[stream]}" ;;
        hpl)
            install -m 0755 "$RUN_ROOT/stage/hpl/xhpl" "${TARGET_FILES[hpl]}"
            install -m 0644 "$RUN_ROOT/stage/hpl/HPL.dat" "${TARGET_FILES[hpl_dat]}"
            ;;
        hpcg)
            install -m 0755 "$RUN_ROOT/stage/hpcg/xhpcg" "${TARGET_FILES[hpcg]}"
            install -m 0644 "$RUN_ROOT/stage/hpcg/hpcg.dat" "${TARGET_FILES[hpcg_dat]}"
            ;;
    esac
done

install -d -m 0755 "$MANIFEST_DIR" "$FRAGMENT_DIR"
for benchmark in "${SELECTED[@]}"; do
    fragment_temp=$(mktemp "$FRAGMENT_DIR/.$benchmark.XXXXXXXX")
    install -m 0644 "$RUN_ROOT/fragments/$benchmark.json" "$fragment_temp"
    mv -f -- "$fragment_temp" "$FRAGMENT_DIR/$benchmark.json"
done

GENERATED_AT=$(date -u +%Y-%m-%dT%H:%M:%SZ)
MANIFEST_TEMP=$(mktemp "$MANIFEST_DIR/.build-manifest.XXXXXXXX")
{
    printf '{"schema_version":"1","generated_at":'; json_string "$GENERATED_AT"
    printf ',"architecture":'; json_string "$ARCHITECTURE"
    printf ',"output_root":'; json_string "$OUTPUT_ROOT"
    printf ',"build_root":'; json_string "$BUILD_ROOT"
    printf ',"last_selection":['
    comma=
    for benchmark in "${SELECTED[@]}"; do
        printf '%s' "$comma"; json_string "$benchmark"; comma=,
    done
    printf ']'
    printf ',"toolchain":{'
    printf '"cc":{"path":'; json_string "$CC_PATH"; printf ',"version":'; json_string "$CC_VERSION"; printf '}'
    printf ',"cxx":{"path":'; json_string "$CXX_PATH"; printf ',"version":'; json_string "$CXX_VERSION"; printf '}'
    printf ',"mpicc":{"path":'; json_string "$MPICC_PATH"; printf ',"show":'; json_string "$MPICC_SHOW"; printf ',"implementation":'; json_string "$MPICC_IMPLEMENTATION"; printf '}'
    printf ',"mpicxx":{"path":'; json_string "$MPICXX_PATH"; printf ',"show":'; json_string "$MPICXX_SHOW"; printf ',"implementation":'; json_string "$MPICXX_IMPLEMENTATION"; printf '}'
    printf ',"mpirun":{"path":'; json_string "$MPIRUN_PATH"; printf ',"version":'; json_string "$MPIRUN_VERSION"; printf ',"implementation":'; json_string "$MPIRUN_IMPLEMENTATION"; printf '}'
    printf ',"openblas":{"include":'; json_string "$OPENBLAS_INCLUDE"; printf ',"library_directory":'; json_string "$OPENBLAS_LIB"; printf ',"library":'; json_string "${OPENBLAS_LIBRARY-}"; printf '}}'
    printf ',"benchmarks":{'
    comma=
    for benchmark in stream hpl hpcg; do
        fragment="$FRAGMENT_DIR/$benchmark.json"
        [ -r "$fragment" ] || continue
        printf '%s' "$comma"; json_string "$benchmark"; printf ':'
        tr -d '\n' <"$fragment"
        comma=,
    done
    printf '}}\n'
} >"$MANIFEST_TEMP"
chmod 0644 "$MANIFEST_TEMP"
mv -f -- "$MANIFEST_TEMP" "$MANIFEST_PATH"

log "build complete"
printf 'Runtime assets: %s\n' "$OUTPUT_ROOT"
printf 'Build manifest: %s\n' "$MANIFEST_PATH"
