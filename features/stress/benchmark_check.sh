#!/usr/bin/env bash
#
# Host adapter template for the CATMonitor stress feature.
#
# Copy this file for the target Linux host and set the absolute paths, thread
# counts, MPI process counts, NUMA policy and benchmark arguments below. These
# execution details intentionally stay in this script rather than YAML or Web
# requests.
#
# The bundled HPL/HPCG commands deliberately use only the launcher arguments
# shared by MPICH/Hydra and OpenMPI: exported environment variables and -np.
# Keep vendor-specific mapping, binding, transport and root options in the
# deployed host copy only after validating them against that host's launcher.

CATMONITOR_STRESS_DESCRIBE_PROTOCOL=1

STREAM_EXECUTABLE=""
STREAM_NUMACTL=""
STREAM_THREADS=0

HPL_WORKDIR=""
HPL_EXECUTABLE=""
HPL_LIBRARY_DIR=""
HPL_MPI_LAUNCHER=""
HPL_MPI_PROCESSES=0
HPL_THREADS_PER_PROCESS=0

HPCG_WORKDIR=""
HPCG_EXECUTABLE=""
HPCG_MPI_LAUNCHER=""
HPCG_MPI_PROCESSES=0
HPCG_THREADS_PER_PROCESS=0
HPCG_NX=32
HPCG_NY=32
HPCG_NZ=32
HPCG_RUNTIME_SECONDS=60

require_absolute_executable() {
    benchmark_name=$1
    executable=$2
    case "$executable" in
        /*) ;;
        *)
            echo "$benchmark_name executable is not configured with an absolute path."
            exit 1
            ;;
    esac
    if [ ! -x "$executable" ]; then
        echo "$benchmark_name executable is unavailable: $executable"
        exit 1
    fi
}

require_absolute_directory() {
    directory_name=$1
    directory_path=$2
    case "$directory_path" in
        /*) ;;
        *)
            echo "$directory_name is not configured with an absolute path."
            exit 1
            ;;
    esac
    if [ ! -d "$directory_path" ]; then
        echo "$directory_name is unavailable: $directory_path"
        exit 1
    fi
}

require_positive_integer() {
    name=$1
    value=$2
    case "$value" in
        ''|*[!0-9]*|0)
            echo "$name must be configured as a positive integer."
            exit 1
            ;;
    esac
}

require_nonnegative_integer() {
    name=$1
    value=$2
    case "$value" in
        ''|*[!0-9]*)
            echo "$name must be configured as a non-negative integer."
            exit 1
            ;;
    esac
}

json_escape() {
    value=${1-}
    value=${value//\\/\\\\}
    value=${value//\"/\\\"}
    value=${value//$'\n'/\\n}
    value=${value//$'\r'/\\r}
    value=${value//$'\t'/\\t}
    value=${value//$'\b'/\\b}
    value=${value//$'\f'/\\f}
    printf '%s' "$value"
}

is_positive_integer() {
    case "$1" in
        ''|*[!0-9]*|0) return 1 ;;
        *) return 0 ;;
    esac
}

is_nonnegative_integer() {
    case "$1" in
        ''|*[!0-9]*) return 1 ;;
        *) return 0 ;;
    esac
}

json_string() {
    printf '"'
    json_escape "${1-}"
    printf '"'
}

file_sha256() {
    file_path=$1
    if [ -f "$file_path" ] && hash sha256sum 2>/dev/null; then
        sha256sum "$file_path" 2>/dev/null | awk '{print $1}'
    fi
}

# emit_asset writes one strict JSON object and returns non-zero only when a
# required asset is unavailable. It never creates or modifies a file.
emit_asset() {
    asset_name=$1
    asset_path=$2
    asset_kind=$3
    asset_required=$4
    asset_status=pass
    asset_message="available"
    case "$asset_kind" in
        executable)
            case "$asset_path" in
                /*) ;;
                *) asset_status=fail; asset_message="path is not absolute" ;;
            esac
            if [ "$asset_status" = pass ] && [ ! -x "$asset_path" ]; then
                asset_status=fail
                asset_message="executable is unavailable"
            fi
            ;;
        directory)
            case "$asset_path" in
                /*) ;;
                *) asset_status=fail; asset_message="path is not absolute" ;;
            esac
            if [ "$asset_status" = pass ] && [ ! -d "$asset_path" ]; then
                asset_status=fail
                asset_message="directory is unavailable"
            fi
            ;;
        file)
            case "$asset_path" in
                /*) ;;
                *) asset_status=fail; asset_message="path is not absolute" ;;
            esac
            if [ "$asset_status" = pass ] && [ ! -r "$asset_path" ]; then
                asset_status=fail
                asset_message="file is unavailable"
            fi
            ;;
        *)
            asset_status=fail
            asset_message="unknown asset kind"
            ;;
    esac
    asset_hash=$(file_sha256 "$asset_path")
    printf '{"name":'
    json_string "$asset_name"
    printf ',"path":'
    json_string "$asset_path"
    printf ',"kind":'
    json_string "$asset_kind"
    printf ',"required":%s,"status":' "$asset_required"
    json_string "$asset_status"
    printf ',"message":'
    json_string "$asset_message"
    if [ -n "$asset_hash" ]; then
        printf ',"sha256":'
        json_string "$asset_hash"
    fi
    printf '}'
    [ "$asset_status" = pass ] || [ "$asset_required" = false ]
}

probe_mpi() {
    mpi_launcher=$1
    mpi_executable=$2
    MPI_IMPLEMENTATION=unknown
    MPI_VERSION=""
    MPI_EXECUTABLE_ABI=unknown
    MPI_STATUS=warn
    MPI_MESSAGE="MPI implementation or executable ABI could not be identified"

    if [ ! -x "$mpi_launcher" ]; then
        MPI_STATUS=fail
        MPI_MESSAGE="MPI launcher is unavailable"
        return
    fi

    MPI_VERSION=$("$mpi_launcher" --version 2>&1)
    mpi_version_status=$?
    MPI_VERSION=${MPI_VERSION:0:1024}
    if [ "$mpi_version_status" -ne 0 ]; then
        MPI_STATUS=fail
        MPI_MESSAGE="MPI launcher version probe failed"
        return
    fi
    mpi_version_lower=${MPI_VERSION,,}
    case "$mpi_version_lower" in
        *"open mpi"*|*"openrte"*) MPI_IMPLEMENTATION=openmpi ;;
        *"mpich"*|*"hydra"*) MPI_IMPLEMENTATION=mpich ;;
    esac

    if hash ldd 2>/dev/null && [ -x "$mpi_executable" ]; then
        mpi_linkage=$(ldd "$mpi_executable" 2>&1)
        mpi_linkage_lower=${mpi_linkage,,}
        case "$mpi_linkage_lower" in
            *libmpich*|*"mpich"*) MPI_EXECUTABLE_ABI=mpich ;;
            *libmpi_usempif*|*libmpi_mpifh*|*libopen-rte*|*libopen-pal*) MPI_EXECUTABLE_ABI=openmpi ;;
        esac
    fi

    if [ "$MPI_IMPLEMENTATION" != unknown ] &&
       [ "$MPI_EXECUTABLE_ABI" != unknown ]; then
        if [ "$MPI_IMPLEMENTATION" = "$MPI_EXECUTABLE_ABI" ]; then
            MPI_STATUS=pass
            MPI_MESSAGE="launcher implementation matches executable MPI ABI"
        else
            MPI_STATUS=fail
            MPI_MESSAGE="launcher implementation does not match executable MPI ABI"
        fi
    elif [ "$MPI_IMPLEMENTATION" != unknown ]; then
        MPI_STATUS=warn
        MPI_MESSAGE="launcher identified; executable MPI ABI is static or could not be identified"
    fi
}

emit_mpi() {
    mpi_required=$1
    printf '{"required":%s,"launcher":' "$mpi_required"
    json_string "${2-}"
    printf ',"implementation":'
    json_string "${MPI_IMPLEMENTATION-unknown}"
    printf ',"version":'
    json_string "${MPI_VERSION-}"
    printf ',"executable_abi":'
    json_string "${MPI_EXECUTABLE_ABI-unknown}"
    printf ',"status":'
    json_string "${MPI_STATUS-pass}"
    printf ',"message":'
    json_string "${MPI_MESSAGE-not required}"
    printf '}'
}

emit_parameter() {
    parameter_key=$1
    parameter_label=$2
    parameter_value=$3
    parameter_unit=${4-}
    printf '{"key":'
    json_string "$parameter_key"
    printf ',"label":'
    json_string "$parameter_label"
    printf ',"value":'
    json_string "$parameter_value"
    if [ -n "$parameter_unit" ]; then
        printf ',"unit":'
        json_string "$parameter_unit"
    fi
    printf '}'
}

read_hpl_dimensions() {
    HPL_N=""
    HPL_NB=""
    HPL_P=""
    HPL_Q=""
    hpl_input=$1
    if [ ! -r "$hpl_input" ]; then
        return
    fi
    hpl_dimensions=$(awk '
        NF {
            count++
            if (count == 6) n=$1
            if (count == 8) nb=$1
            if (count == 11) p=$1
            if (count == 12) q=$1
        }
        END {
            if (n ~ /^[0-9]+$/ && nb ~ /^[0-9]+$/ &&
                p ~ /^[0-9]+$/ && q ~ /^[0-9]+$/) {
                print n, nb, p, q
            }
        }
    ' "$hpl_input")
    if [ -n "$hpl_dimensions" ]; then
        read -r HPL_N HPL_NB HPL_P HPL_Q <<< "$hpl_dimensions"
    fi
}

emit_preflight() {
    failed=$1
    warned=$2
    if [ "$failed" -gt 0 ]; then
        preflight_status=fail
        preflight_message="$failed required preflight check(s) failed"
    elif [ "$warned" -gt 0 ]; then
        preflight_status=warn
        preflight_message="required assets are available; $warned compatibility check(s) need review"
    else
        preflight_status=pass
        preflight_message="required assets and compatibility checks passed"
    fi
    printf '{"status":'
    json_string "$preflight_status"
    printf ',"message":'
    json_string "$preflight_message"
    printf '}'
}

describe_stream() {
    failed=0
    stream_workers=$STREAM_THREADS
    if ! is_nonnegative_integer "$STREAM_THREADS"; then
        failed=$((failed + 1))
        stream_workers=0
    fi
    MPI_IMPLEMENTATION=none
    MPI_VERSION=""
    MPI_EXECUTABLE_ABI=none
    MPI_STATUS=pass
    MPI_MESSAGE="MPI is not used by STREAM"
    printf '{"protocol_version":1,"benchmark":"stream","parameters":['
    emit_parameter executable "Executable" "$STREAM_EXECUTABLE"
    printf ','
    emit_parameter threads "OpenMP threads" "$STREAM_THREADS" threads
    printf ','
    emit_parameter numa_policy "NUMA policy" interleave_all
    printf '],"resources":{"mpi_processes":0,"threads_per_process":%s,"total_workers":%s,"runtime_seconds":0,"problem_size":""},"assets":[' \
        "$stream_workers" "$stream_workers"
    if ! emit_asset executable "$STREAM_EXECUTABLE" executable true; then failed=$((failed + 1)); fi
    printf ','
    if ! emit_asset numa_launcher "$STREAM_NUMACTL" executable true; then failed=$((failed + 1)); fi
    printf '],"mpi":'
    emit_mpi false ""
    printf ',"preflight":'
    emit_preflight "$failed" 0
    printf '}\n'
}

describe_hpl() {
    failed=0
    warned=0
    hpl_processes=$HPL_MPI_PROCESSES
    hpl_threads=$HPL_THREADS_PER_PROCESS
    if ! is_positive_integer "$hpl_processes"; then
        failed=$((failed + 1))
        hpl_processes=0
    fi
    if ! is_positive_integer "$hpl_threads"; then
        failed=$((failed + 1))
        hpl_threads=0
    fi
    hpl_input="$HPL_WORKDIR/HPL.dat"
    read_hpl_dimensions "$hpl_input"
    hpl_process_grid=""
    if [ -n "$HPL_P" ] && [ -n "$HPL_Q" ]; then
        hpl_process_grid="${HPL_P}x${HPL_Q}"
    fi
    probe_mpi "$HPL_MPI_LAUNCHER" "$HPL_EXECUTABLE"
    case "$MPI_STATUS" in
        fail) failed=$((failed + 1)) ;;
        warn) warned=$((warned + 1)) ;;
    esac
    total_workers=$((hpl_processes * hpl_threads))
    printf '{"protocol_version":1,"benchmark":"hpl","parameters":['
    emit_parameter executable "Executable" "$HPL_EXECUTABLE"
    printf ','
    emit_parameter mpi_processes "MPI processes" "$HPL_MPI_PROCESSES" ranks
    printf ','
    emit_parameter threads_per_process "Threads per process" "$HPL_THREADS_PER_PROCESS" threads
    printf ','
    emit_parameter n "Problem size N" "$HPL_N"
    printf ','
    emit_parameter nb "Block size NB" "$HPL_NB"
    printf ','
    emit_parameter process_grid "Process grid P x Q" "$hpl_process_grid"
    printf '],"resources":{"mpi_processes":%s,"threads_per_process":%s,"total_workers":%s,"runtime_seconds":0,"problem_size":' \
        "$hpl_processes" "$hpl_threads" "$total_workers"
    json_string "${HPL_N:-unknown}"
    printf '},"assets":['
    if ! emit_asset executable "$HPL_EXECUTABLE" executable true; then failed=$((failed + 1)); fi
    printf ','
    if ! emit_asset working_directory "$HPL_WORKDIR" directory true; then failed=$((failed + 1)); fi
    printf ','
    if ! emit_asset input_file "$hpl_input" file true; then failed=$((failed + 1)); fi
    printf ','
    if ! emit_asset mpi_launcher "$HPL_MPI_LAUNCHER" executable true; then failed=$((failed + 1)); fi
    if [ -n "$HPL_LIBRARY_DIR" ]; then
        printf ','
        if ! emit_asset library_directory "$HPL_LIBRARY_DIR" directory true; then failed=$((failed + 1)); fi
    fi
    printf '],"mpi":'
    emit_mpi true "$HPL_MPI_LAUNCHER"
    printf ',"preflight":'
    emit_preflight "$failed" "$warned"
    printf '}\n'
}

describe_hpcg() {
    failed=0
    warned=0
    hpcg_processes=$HPCG_MPI_PROCESSES
    hpcg_threads=$HPCG_THREADS_PER_PROCESS
    hpcg_runtime=$HPCG_RUNTIME_SECONDS
    hpcg_nx=$HPCG_NX
    hpcg_ny=$HPCG_NY
    hpcg_nz=$HPCG_NZ
    if ! is_positive_integer "$hpcg_processes"; then failed=$((failed + 1)); hpcg_processes=0; fi
    if ! is_positive_integer "$hpcg_threads"; then failed=$((failed + 1)); hpcg_threads=0; fi
    if ! is_positive_integer "$hpcg_runtime"; then failed=$((failed + 1)); hpcg_runtime=0; fi
    if ! is_positive_integer "$hpcg_nx"; then failed=$((failed + 1)); hpcg_nx=0; fi
    if ! is_positive_integer "$hpcg_ny"; then failed=$((failed + 1)); hpcg_ny=0; fi
    if ! is_positive_integer "$hpcg_nz"; then failed=$((failed + 1)); hpcg_nz=0; fi
    probe_mpi "$HPCG_MPI_LAUNCHER" "$HPCG_EXECUTABLE"
    case "$MPI_STATUS" in
        fail) failed=$((failed + 1)) ;;
        warn) warned=$((warned + 1)) ;;
    esac
    total_workers=$((hpcg_processes * hpcg_threads))
    printf '{"protocol_version":1,"benchmark":"hpcg","parameters":['
    emit_parameter executable "Executable" "$HPCG_EXECUTABLE"
    printf ','
    emit_parameter mpi_processes "MPI processes" "$HPCG_MPI_PROCESSES" ranks
    printf ','
    emit_parameter threads_per_process "Threads per process" "$HPCG_THREADS_PER_PROCESS" threads
    printf ','
    emit_parameter local_grid "Local grid" "${hpcg_nx}x${hpcg_ny}x${hpcg_nz}"
    printf ','
    emit_parameter target_runtime "Target runtime" "$HPCG_RUNTIME_SECONDS" seconds
    printf '],"resources":{"mpi_processes":%s,"threads_per_process":%s,"total_workers":%s,"runtime_seconds":%s,"problem_size":' \
        "$hpcg_processes" "$hpcg_threads" "$total_workers" "$hpcg_runtime"
    json_string "${hpcg_nx}x${hpcg_ny}x${hpcg_nz}"
    printf '},"assets":['
    if ! emit_asset executable "$HPCG_EXECUTABLE" executable true; then failed=$((failed + 1)); fi
    printf ','
    if ! emit_asset working_directory "$HPCG_WORKDIR" directory true; then failed=$((failed + 1)); fi
    printf ','
    if ! emit_asset mpi_launcher "$HPCG_MPI_LAUNCHER" executable true; then failed=$((failed + 1)); fi
    printf '],"mpi":'
    emit_mpi true "$HPCG_MPI_LAUNCHER"
    printf ',"preflight":'
    emit_preflight "$failed" "$warned"
    printf '}\n'
}

if [ "$#" -lt 1 ]; then
    echo "Insufficient number of parameters."
    exit 1
fi

benchmark_type=$1
shift

case "$benchmark_type" in
    describe)
        if [ "$#" -ne 1 ]; then
            echo "describe requires exactly one benchmark name."
            exit 1
        fi
        case "$1" in
            stream) describe_stream ;;
            hpl) describe_hpl ;;
            hpcg) describe_hpcg ;;
            *) echo "Unknown benchmark for describe."; exit 1 ;;
        esac
        ;;
    stream)
        if [ "$#" -ne 0 ]; then exit 1; fi
        require_absolute_executable "STREAM" "$STREAM_EXECUTABLE"
        require_absolute_executable "STREAM NUMA launcher" "$STREAM_NUMACTL"
        require_nonnegative_integer "STREAM_THREADS" "$STREAM_THREADS"
        if [ "$STREAM_THREADS" -gt 0 ]; then
            export OMP_NUM_THREADS="$STREAM_THREADS"
        fi
        exec "$STREAM_NUMACTL" --interleave=all "$STREAM_EXECUTABLE"
        ;;
    hpl)
        if [ "$#" -ne 0 ]; then exit 1; fi
        require_absolute_executable "HPL" "$HPL_EXECUTABLE"
        require_absolute_executable "HPL MPI launcher" "$HPL_MPI_LAUNCHER"
        require_absolute_directory "HPL working directory" "$HPL_WORKDIR"
        require_positive_integer "HPL_MPI_PROCESSES" "$HPL_MPI_PROCESSES"
        require_positive_integer "HPL_THREADS_PER_PROCESS" "$HPL_THREADS_PER_PROCESS"
        hpl_input="$HPL_WORKDIR/HPL.dat"
        if [ ! -r "$hpl_input" ]; then
            echo "HPL input file is unavailable: $hpl_input"
            exit 1
        fi
        if [ -n "$HPL_LIBRARY_DIR" ]; then
            require_absolute_directory "HPL library directory" "$HPL_LIBRARY_DIR"
            export LD_LIBRARY_PATH="${HPL_LIBRARY_DIR}${LD_LIBRARY_PATH:+:${LD_LIBRARY_PATH}}"
        fi
        export OPENBLAS_NUM_THREADS="$HPL_THREADS_PER_PROCESS"
        export OMP_NUM_THREADS="$HPL_THREADS_PER_PROCESS"
        cd "$HPL_WORKDIR" || exit 1
        exec "$HPL_MPI_LAUNCHER" \
            -np "$HPL_MPI_PROCESSES" \
            "$HPL_EXECUTABLE"
        ;;
    hpcg)
        if [ "$#" -ne 0 ]; then exit 1; fi
        require_absolute_executable "HPCG" "$HPCG_EXECUTABLE"
        require_absolute_executable "HPCG MPI launcher" "$HPCG_MPI_LAUNCHER"
        require_absolute_directory "HPCG working directory" "$HPCG_WORKDIR"
        require_positive_integer "HPCG_MPI_PROCESSES" "$HPCG_MPI_PROCESSES"
        require_positive_integer "HPCG_THREADS_PER_PROCESS" "$HPCG_THREADS_PER_PROCESS"
        require_positive_integer "HPCG_NX" "$HPCG_NX"
        require_positive_integer "HPCG_NY" "$HPCG_NY"
        require_positive_integer "HPCG_NZ" "$HPCG_NZ"
        require_positive_integer "HPCG_RUNTIME_SECONDS" "$HPCG_RUNTIME_SECONDS"
        export OMP_NUM_THREADS="$HPCG_THREADS_PER_PROCESS"
        export OMP_DYNAMIC=FALSE
        cd "$HPCG_WORKDIR" || exit 1
        exec "$HPCG_MPI_LAUNCHER" \
            -np "$HPCG_MPI_PROCESSES" \
            "$HPCG_EXECUTABLE" \
            --nx="$HPCG_NX" \
            --ny="$HPCG_NY" \
            --nz="$HPCG_NZ" \
            --rt="$HPCG_RUNTIME_SECONDS"
        ;;
    *)
        echo "Unknown parameter."
        exit 1
        ;;
esac
