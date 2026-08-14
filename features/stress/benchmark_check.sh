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

# MindCluster Ascend NPU Burn source is vendored under its Mulan PSL v2 license.
# The node administrator still owns the native/container runtime environment;
# CATMonitor only invokes the fixed adapter below and parses its result.
# Container image/device/volume settings intentionally do not enter CATMonitor
# YAML or Web requests.
#
# Supported template backends:
#   native      - execute NPU_BURN_EXECUTABLE on the host.
#   docker_exec - execute it in an already running, administrator-maintained
#                 container. The result directory must be visible on the host.
NPU_BURN_BACKEND="native"
NPU_BURN_EXECUTABLE=""
NPU_BURN_CONTAINER_RUNTIME=""
NPU_BURN_CONTAINER_NAME=""
NPU_BURN_CONTAINER_IMAGE=""
NPU_BURN_RUNTIME_CANN=""
NPU_BURN_RUNTIME_TORCH_NPU=""
NPU_BURN_SOC_MODEL=""
# Host-visible result directory. The adapter deliberately does not pass the
# upstream --output option: the bundled release rejects an existing custom
# directory. Native execution must use the tool default below; the repository
# container bootstrap binds this host directory to that default in the image.
NPU_BURN_OUTPUT_DIR="${HOME}/.ascend_npu_burn/output"
NPU_BURN_RUN_CASE=""
NPU_BURN_GROUP=""
# Deliberately empty: an administrator must explicitly reserve one or more
# logical devices for stress. "all" is supported by upstream, but is unsafe as
# an implicit default on shared nodes.
NPU_BURN_DEVICE=""
NPU_BURN_DEVICE_ROOT="/dev"
NPU_BURN_INTERNAL_TIMEOUT_SECONDS=300
NPU_BURN_CHIP_GENERATION=""

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

# probe_npu_container performs read-only readiness checks for docker_exec. It
# does not create, start, stop, or remove containers.
probe_npu_container() {
    NPU_CONTAINER_STATUS=fail
    NPU_CONTAINER_MESSAGE="container backend is not configured"
    NPU_CONTAINER_DETECTED_IMAGE=""
    if [ ! -x "$NPU_BURN_CONTAINER_RUNTIME" ]; then
        NPU_CONTAINER_MESSAGE="container runtime is unavailable"
        return
    fi
    case "$NPU_BURN_CONTAINER_NAME" in
        ''|-*|*[!A-Za-z0-9_.-]*)
            NPU_CONTAINER_MESSAGE="container name is empty or invalid"
            return
            ;;
    esac
    container_probe=$(
        "$NPU_BURN_CONTAINER_RUNTIME" inspect \
            --format '{{.State.Running}}|{{.Config.Image}}' \
            "$NPU_BURN_CONTAINER_NAME" 2>/dev/null
    )
    if [ $? -ne 0 ]; then
        NPU_CONTAINER_MESSAGE="container is unavailable"
        return
    fi
    container_running=${container_probe%%|*}
    NPU_CONTAINER_DETECTED_IMAGE=${container_probe#*|}
    if [ "$container_running" != true ]; then
        NPU_CONTAINER_MESSAGE="container is not running"
        return
    fi
    case "$NPU_BURN_EXECUTABLE" in
        /*) ;;
        *) NPU_CONTAINER_MESSAGE="container executable path is not absolute"; return ;;
    esac
    if ! "$NPU_BURN_CONTAINER_RUNTIME" exec "$NPU_BURN_CONTAINER_NAME" \
        /bin/sh -c '/usr/bin/test -x "$1"' catmonitor "$NPU_BURN_EXECUTABLE" \
        >/dev/null 2>&1; then
        NPU_CONTAINER_MESSAGE="container executable is unavailable"
        return
    fi
    NPU_CONTAINER_STATUS=pass
    NPU_CONTAINER_MESSAGE="running container and executable are available"
}

emit_npu_container_asset() {
    printf '{"name":"container","path":'
    json_string "$NPU_BURN_CONTAINER_NAME"
    printf ',"kind":"container","required":true,"status":'
    json_string "$NPU_CONTAINER_STATUS"
    printf ',"message":'
    json_string "$NPU_CONTAINER_MESSAGE"
    printf '}'
    [ "$NPU_CONTAINER_STATUS" = pass ]
}

# probe_npu_logical_devices starts from /dev/davinciN names. For docker_exec it
# also reproduces the upstream lspci filter and requires both logical ID sets
# to match. It deliberately does not use the PyTorch-reported device count or
# npu-smi Phy-ID because those are different namespaces on multi-die systems.
probe_npu_logical_devices() {
    NPU_DEVICE_STATUS=fail
    NPU_DEVICE_MESSAGE="NPU Burn logical device topology is unavailable"
    NPU_AVAILABLE_DEVICES=""
    NPU_PCI_TOPOLOGY_DEVICES=""
    NPU_TOPOLOGY_SOURCE="device_nodes"
    NPU_DEVICE_ASSET_PATH="$NPU_BURN_DEVICE_ROOT/davinci[0-9]*"
    device_lines=""
    case "$NPU_BURN_BACKEND" in
        docker_exec)
            NPU_DEVICE_ASSET_PATH="/dev/davinci[0-9]*"
            NPU_TOPOLOGY_SOURCE="container_lspci"
            if [ "${NPU_CONTAINER_STATUS-}" != pass ]; then
                NPU_DEVICE_MESSAGE="fixed container is not ready for logical device discovery"
                return
            fi
            device_lines=$(
                "$NPU_BURN_CONTAINER_RUNTIME" exec "$NPU_BURN_CONTAINER_NAME" \
                    /bin/sh -c '
                        for path in /dev/davinci[0-9]*; do
                            name=${path##*/}
                            id=${name#davinci}
                            case "$id" in ""|*[!0-9]*) continue ;; esac
                            printf "%s\n" "$id"
                        done
                    ' 2>/dev/null
            ) || {
                NPU_DEVICE_MESSAGE="cannot inspect /dev/davinciN in the fixed container"
                return
            }
            pci_topology_lines=$(
                "$NPU_BURN_CONTAINER_RUNTIME" exec "$NPU_BURN_CONTAINER_NAME" \
                    /bin/sh -c '
                        lspci_path=$(command -v lspci) || exit 127
                        output=$("$lspci_path" -D -d 19e5:) || exit 70
                        printf "%s\n" "$output" |
                            LC_ALL=C sort |
                            awk '\''/Processing accelerators/ && /Device/ { print count; count++ }'\''
                    ' 2>/dev/null
            ) || {
                NPU_DEVICE_MESSAGE="cannot enumerate NPU Burn PCI topology in the fixed container; ensure pciutils/lspci is installed and executable"
                return
            }
            NPU_PCI_TOPOLOGY_DEVICES=$(
                printf '%s' "$pci_topology_lines" |
                    awk '/^[0-9]+$/ { print $1 }' |
                    sort -n -u |
                    paste -sd, -
            )
            if [ -z "$NPU_PCI_TOPOLOGY_DEVICES" ]; then
                NPU_DEVICE_MESSAGE="lspci found no Ascend 19e5 Processing accelerators in the fixed container; refusing the upstream eight-device fallback"
                return
            fi
            ;;
        native)
            case "$NPU_BURN_DEVICE_ROOT" in
                /*) ;;
                *)
                    NPU_DEVICE_MESSAGE="NPU_BURN_DEVICE_ROOT must be an absolute path"
                    return
                    ;;
            esac
            for path in "$NPU_BURN_DEVICE_ROOT"/davinci[0-9]*; do
                name=${path##*/}
                id=${name#davinci}
                case "$id" in ""|*[!0-9]*) continue ;; esac
                device_lines+="$id"$'\n'
            done
            ;;
        *)
            NPU_DEVICE_MESSAGE="NPU_BURN_BACKEND must be native or docker_exec"
            return
            ;;
    esac
    NPU_AVAILABLE_DEVICES=$(
        printf '%s' "$device_lines" |
            awk '/^[0-9]+$/ { print $1 }' |
            sort -n -u |
            paste -sd, -
    )
    if [ -z "$NPU_AVAILABLE_DEVICES" ]; then
        NPU_DEVICE_MESSAGE="no /dev/davinciN logical device nodes are available"
        return
    fi
    if [ "$NPU_BURN_BACKEND" = docker_exec ] &&
       [ "$NPU_AVAILABLE_DEVICES" != "$NPU_PCI_TOPOLOGY_DEVICES" ]; then
        NPU_DEVICE_MESSAGE="container device nodes ($NPU_AVAILABLE_DEVICES) do not match NPU Burn lspci topology logical IDs ($NPU_PCI_TOPOLOGY_DEVICES); verify pciutils and the fixed-container device mapping"
        return
    fi
    case "$NPU_BURN_DEVICE" in
        all)
            NPU_DEVICE_STATUS=pass
            NPU_DEVICE_MESSAGE="all available NPU Burn logical devices: $NPU_AVAILABLE_DEVICES"
            return
            ;;
        ''|,*|*,|*,,*)
            NPU_DEVICE_MESSAGE="NPU_BURN_DEVICE must explicitly select one or more logical device IDs (for example 7 or 0,1,7); use all only on an exclusively reserved node"
            return
            ;;
    esac
    seen_devices=,
    IFS=',' read -r -a requested_devices <<<"$NPU_BURN_DEVICE"
    for requested_device in "${requested_devices[@]}"; do
        case "$requested_device" in
            ''|*[!0-9]*)
                NPU_DEVICE_MESSAGE="NPU_BURN_DEVICE must be all or a comma-separated list of logical device IDs without whitespace"
                return
                ;;
        esac
        case "$seen_devices" in
            *",$requested_device,"*)
                NPU_DEVICE_MESSAGE="NPU_BURN_DEVICE contains duplicate logical device $requested_device"
                return
                ;;
        esac
        seen_devices+="$requested_device,"
        case ",$NPU_AVAILABLE_DEVICES," in
            *",$requested_device,"*) ;;
            *)
                NPU_DEVICE_MESSAGE="NPU Burn logical device $requested_device is unavailable; valid logical devices: $NPU_AVAILABLE_DEVICES. Do not use npu-smi Phy-ID as NPU_BURN_DEVICE."
                return
                ;;
        esac
    done
    NPU_DEVICE_STATUS=pass
    NPU_DEVICE_MESSAGE="selected NPU Burn logical devices are available: $NPU_BURN_DEVICE"
}

emit_npu_devices_asset() {
    printf '{"name":"logical_devices","path":'
    json_string "$NPU_DEVICE_ASSET_PATH"
    printf ',"kind":"device_topology","required":true,"status":'
    json_string "$NPU_DEVICE_STATUS"
    printf ',"message":'
    json_string "$NPU_DEVICE_MESSAGE"
    printf '}'
    [ "$NPU_DEVICE_STATUS" = pass ]
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

describe_npu_burn() {
    failed=0
    warned=0
    case "$NPU_BURN_BACKEND" in
        native) ;;
        docker_exec)
            probe_npu_container
            if [ "$NPU_CONTAINER_STATUS" != pass ]; then failed=$((failed + 1)); fi
            if [ -n "$NPU_BURN_CONTAINER_IMAGE" ] &&
               [ -n "$NPU_CONTAINER_DETECTED_IMAGE" ] &&
               [ "$NPU_BURN_CONTAINER_IMAGE" != "$NPU_CONTAINER_DETECTED_IMAGE" ]; then
                failed=$((failed + 1))
                NPU_CONTAINER_STATUS=fail
                NPU_CONTAINER_MESSAGE="running image does not match the declared image"
            fi
            if [ -z "$NPU_BURN_RUNTIME_CANN" ] ||
               [ -z "$NPU_BURN_RUNTIME_TORCH_NPU" ] ||
               [ -z "$NPU_BURN_SOC_MODEL" ]; then
                warned=$((warned + 1))
            fi
            ;;
        *) failed=$((failed + 1)) ;;
    esac
    probe_npu_logical_devices
    if [ "$NPU_DEVICE_STATUS" != pass ]; then failed=$((failed + 1)); fi
    if ! is_positive_integer "$NPU_BURN_INTERNAL_TIMEOUT_SECONDS"; then failed=$((failed + 1)); fi
    if [ -z "$NPU_BURN_DEVICE" ]; then failed=$((failed + 1)); fi
    npu_output_mode=upstream_default
    npu_tool_output_dir="${HOME}/.ascend_npu_burn/output"
    if [ "$NPU_BURN_BACKEND" = docker_exec ]; then
        npu_tool_output_dir=/opt/catmonitor/npuburn-home/.ascend_npu_burn/output
    fi
    case "$NPU_BURN_CHIP_GENERATION" in
        A2|A3|A5) ;;
        *) failed=$((failed + 1)) ;;
    esac
    if { [ -n "$NPU_BURN_RUN_CASE" ] && [ -n "$NPU_BURN_GROUP" ]; } ||
       { [ -z "$NPU_BURN_RUN_CASE" ] && [ -z "$NPU_BURN_GROUP" ]; }; then
        failed=$((failed + 1))
    fi
    npu_workload=$NPU_BURN_RUN_CASE
    npu_selector=run_case
    if [ -n "$NPU_BURN_GROUP" ]; then
        npu_workload=$NPU_BURN_GROUP
        npu_selector=group
    fi
    MPI_IMPLEMENTATION=none
    MPI_VERSION=""
    MPI_EXECUTABLE_ABI=none
    MPI_STATUS=pass
    MPI_MESSAGE="MPI is not used by Ascend NPU Burn"
    printf '{"protocol_version":1,"benchmark":"npu_burn","parameters":['
    emit_parameter backend "Execution backend" "$NPU_BURN_BACKEND"
    printf ','
    emit_parameter executable "Executable" "$NPU_BURN_EXECUTABLE"
    if [ "$NPU_BURN_BACKEND" = docker_exec ]; then
        npu_container_image=$NPU_BURN_CONTAINER_IMAGE
        if [ -z "$npu_container_image" ]; then
            npu_container_image=$NPU_CONTAINER_DETECTED_IMAGE
        fi
        printf ','
        emit_parameter container "Container" "$NPU_BURN_CONTAINER_NAME"
        printf ','
        emit_parameter image "Container image" "$npu_container_image"
        printf ','
        emit_parameter cann "CANN" "$NPU_BURN_RUNTIME_CANN"
        printf ','
        emit_parameter torch_npu "torch_npu" "$NPU_BURN_RUNTIME_TORCH_NPU"
        printf ','
        emit_parameter soc "NPU SoC" "$NPU_BURN_SOC_MODEL"
    fi
    printf ','
    emit_parameter output_mode "Output mode" "$npu_output_mode"
    printf ','
    emit_parameter tool_output_directory "Tool output directory" "$npu_tool_output_dir"
    printf ','
    emit_parameter result_directory "Host-visible result directory" "$NPU_BURN_OUTPUT_DIR"
    printf ','
    emit_parameter selector "Workload selector" "$npu_selector"
    printf ','
    emit_parameter workload "Run case / group" "$npu_workload"
    printf ','
    emit_parameter devices "NPU devices" "$NPU_BURN_DEVICE"
    printf ','
    emit_parameter device_namespace "Device namespace" npu_burn_logical
    printf ','
    emit_parameter available_devices "Available logical devices" "$NPU_AVAILABLE_DEVICES"
    printf ','
    emit_parameter topology_source "Topology source" "$NPU_TOPOLOGY_SOURCE"
    printf ','
    emit_parameter pci_topology_devices "PCI topology logical devices" "$NPU_PCI_TOPOLOGY_DEVICES"
    printf ','
    emit_parameter chip_generation "Chip generation" "$NPU_BURN_CHIP_GENERATION"
    printf ','
    emit_parameter sdc_detection "SDC detection" enabled
    printf ','
    emit_parameter internal_timeout "Per-case timeout" "$NPU_BURN_INTERNAL_TIMEOUT_SECONDS" seconds
    printf '],"resources":{"mpi_processes":0,"threads_per_process":0,"total_workers":0,"runtime_seconds":%s,"problem_size":' \
        "$NPU_BURN_INTERNAL_TIMEOUT_SECONDS"
    json_string "$npu_workload"
    printf '},"assets":['
    if [ "$NPU_BURN_BACKEND" = docker_exec ]; then
        if ! emit_asset container_runtime "$NPU_BURN_CONTAINER_RUNTIME" executable true; then failed=$((failed + 1)); fi
        printf ','
        if ! emit_npu_container_asset; then :; fi
    else
        if ! emit_asset executable "$NPU_BURN_EXECUTABLE" executable true; then failed=$((failed + 1)); fi
    fi
    printf ','
    if ! emit_asset output_directory "$NPU_BURN_OUTPUT_DIR" directory true; then failed=$((failed + 1)); fi
    printf ','
    if ! emit_npu_devices_asset; then :; fi
    printf '],"mpi":'
    emit_mpi false ""
    printf ',"preflight":'
    emit_preflight "$failed" "$warned"
    printf '}\n'
}

summarize_npu_burn_csv() {
    result_file=$1
    if [ ! -s "$result_file" ]; then
        echo "Ascend NPU Burn result CSV is missing or empty: $result_file"
        return 1
    fi
    summary=$(awk -F',' '
        NR == 1 && $1 == "task" && $8 == "result" { header=1; next }
        $1 == "task" && $8 == "result" { next }
        NF < 8 { malformed=1; next }
        {
            cases++
            devices[$2]=1
            exetime += $6 + 0
            errors += $7 + 0
            if ($8 == "PASS" && ($7 + 0) == 0) passed++
            else failed++
        }
        END {
            for (device in devices) device_count++
            if (!header || cases == 0 || malformed) exit 2
            printf "CATMONITOR_NPU_BURN_SUMMARY devices=%d cases=%d passed=%d failed=%d errors=%d case_time_seconds=%.6f\n", \
                device_count, cases, passed, failed, errors, exetime
            if (failed > 0 || errors > 0) exit 3
        }
    ' "$result_file")
    summary_status=$?
    if [ -n "$summary" ]; then printf '%s\n' "$summary"; fi
    case "$summary_status" in
        0) return 0 ;;
        2) echo "Ascend NPU Burn result CSV has an invalid schema or no result rows." ;;
        3) echo "Ascend NPU Burn reported failed cases or SDC errors." ;;
        *) echo "Ascend NPU Burn result CSV could not be parsed." ;;
    esac
    return 1
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
            npu_burn) describe_npu_burn ;;
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
    npu_burn)
        if [ "$#" -ne 0 ]; then exit 1; fi
        case "$NPU_BURN_BACKEND" in
            native)
                require_absolute_executable "Ascend NPU Burn" "$NPU_BURN_EXECUTABLE"
                ;;
            docker_exec)
                require_absolute_executable "Ascend NPU Burn container runtime" "$NPU_BURN_CONTAINER_RUNTIME"
                probe_npu_container
                if [ "$NPU_CONTAINER_STATUS" != pass ]; then
                    echo "Ascend NPU Burn container is not ready: $NPU_CONTAINER_MESSAGE"
                    exit 1
                fi
                if [ -n "$NPU_BURN_CONTAINER_IMAGE" ] &&
                   [ "$NPU_BURN_CONTAINER_IMAGE" != "$NPU_CONTAINER_DETECTED_IMAGE" ]; then
                    echo "Ascend NPU Burn container image mismatch: expected $NPU_BURN_CONTAINER_IMAGE, got $NPU_CONTAINER_DETECTED_IMAGE"
                    exit 1
                fi
                ;;
            *) echo "NPU_BURN_BACKEND must be native or docker_exec."; exit 1 ;;
        esac
        probe_npu_logical_devices
        if [ "$NPU_DEVICE_STATUS" != pass ]; then
            echo "$NPU_DEVICE_MESSAGE"
            exit 1
        fi
        require_absolute_directory "Ascend NPU Burn output directory" "$NPU_BURN_OUTPUT_DIR"
        require_positive_integer "NPU_BURN_INTERNAL_TIMEOUT_SECONDS" "$NPU_BURN_INTERNAL_TIMEOUT_SECONDS"
        if [ -z "$NPU_BURN_DEVICE" ]; then
            echo "NPU_BURN_DEVICE must not be empty."
            exit 1
        fi
        case "$NPU_BURN_CHIP_GENERATION" in
            A2|A3|A5) ;;
            *) echo "NPU_BURN_CHIP_GENERATION must be A2, A3, or A5."; exit 1 ;;
        esac
        if { [ -n "$NPU_BURN_RUN_CASE" ] && [ -n "$NPU_BURN_GROUP" ]; } ||
           { [ -z "$NPU_BURN_RUN_CASE" ] && [ -z "$NPU_BURN_GROUP" ]; }; then
            echo "Configure exactly one of NPU_BURN_RUN_CASE or NPU_BURN_GROUP."
            exit 1
        fi
        npu_args=(
            --device "$NPU_BURN_DEVICE"
            # SDC is the reliability verdict implemented by this benchmark,
            # not a workaround for the upstream args.detect initialization bug.
            --sdc_detect
            --timeout "$NPU_BURN_INTERNAL_TIMEOUT_SECONDS"
            --chip_generation "$NPU_BURN_CHIP_GENERATION"
        )
        if [ -n "$NPU_BURN_RUN_CASE" ]; then
            npu_args+=(--run_case "$NPU_BURN_RUN_CASE")
        else
            npu_args+=(--group "$NPU_BURN_GROUP")
        fi
        npu_result_file="$NPU_BURN_OUTPUT_DIR/npu_burn_results.csv"
        npu_result_signature_before=""
        if [ -e "$npu_result_file" ]; then
            npu_result_signature_before=$(stat -c '%y:%s' "$npu_result_file" 2>/dev/null)
        fi
        npu_console_log=$(mktemp "${TMPDIR:-/tmp}/catmonitor-npu-burn.XXXXXX") || exit 1
        trap 'rm -f "$npu_console_log"' EXIT
        npu_command=("$NPU_BURN_EXECUTABLE" "${npu_args[@]}")
        if [ "$NPU_BURN_BACKEND" = docker_exec ]; then
            npu_command=(
                "$NPU_BURN_CONTAINER_RUNTIME" exec "$NPU_BURN_CONTAINER_NAME"
                "${npu_command[@]}"
            )
        fi
        "${npu_command[@]}" 2>&1 | tee "$npu_console_log"
        npu_status=${PIPESTATUS[0]}
        if [ "$npu_status" -ne 0 ]; then
            exit "$npu_status"
        fi
        if grep -Eq '[|][[:space:]]*[0-9]+[[:space:]]*[|][[:space:]]*FAIL[[:space:]]*[|]' "$npu_console_log"; then
            echo "Ascend NPU Burn global device summary reported failure."
            exit 1
        fi
        npu_result_signature_after=$(stat -c '%y:%s' "$npu_result_file" 2>/dev/null)
        if [ -n "$npu_result_signature_before" ] &&
           [ "$npu_result_signature_before" = "$npu_result_signature_after" ]; then
            echo "Ascend NPU Burn did not update its result CSV during this run."
            exit 1
        fi
        summarize_npu_burn_csv "$npu_result_file"
        ;;
    *)
        echo "Unknown parameter."
        exit 1
        ;;
esac
