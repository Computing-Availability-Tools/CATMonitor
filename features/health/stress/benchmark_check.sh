#!/usr/bin/env bash
#
# Host adapter template for CATMonitor health/stress.
#
# Copy this file for the target Linux host and set the absolute paths, thread
# counts, MPI process counts, NUMA policy and benchmark arguments below. These
# execution details intentionally stay in this script rather than YAML or Web
# requests.

STREAM_EXECUTABLE=""
STREAM_THREADS=0

HPL_WORKDIR=""
HPL_EXECUTABLE=""
HPL_INPUT=""
HPL_LIBRARY_DIR=""
HPL_MPI_PROCESSES=0
HPL_THREADS_PER_PROCESS=0

HPCG_WORKDIR=""
HPCG_EXECUTABLE=""
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

if [ "$#" -lt 1 ]; then
    echo "Insufficient number of parameters."
    exit 1
fi

benchmark_type=$1
shift

case "$benchmark_type" in
    stream)
        if [ "$#" -ne 0 ]; then exit 1; fi
        require_absolute_executable "STREAM" "$STREAM_EXECUTABLE"
        if ! command -v numactl >/dev/null 2>&1; then
            echo "STREAM NUMA launcher is unavailable: numactl"
            exit 1
        fi
        if [ "$STREAM_THREADS" -gt 0 ] 2>/dev/null; then
            export OMP_NUM_THREADS="$STREAM_THREADS"
        fi
        numactl --interleave=all "$STREAM_EXECUTABLE"
        ;;
    hpl)
        if [ "$#" -ne 0 ]; then exit 1; fi
        require_absolute_executable "HPL" "$HPL_EXECUTABLE"
        require_positive_integer "HPL_MPI_PROCESSES" "$HPL_MPI_PROCESSES"
        require_positive_integer "HPL_THREADS_PER_PROCESS" "$HPL_THREADS_PER_PROCESS"
        if [ ! -d "$HPL_WORKDIR" ]; then
            echo "HPL working directory is unavailable: $HPL_WORKDIR"
            exit 1
        fi
        if [ ! -r "$HPL_INPUT" ]; then
            echo "HPL input file is unavailable: $HPL_INPUT"
            exit 1
        fi
        if [ -n "$HPL_LIBRARY_DIR" ]; then
            if [ ! -d "$HPL_LIBRARY_DIR" ]; then
                echo "HPL library directory is unavailable: $HPL_LIBRARY_DIR"
                exit 1
            fi
            export LD_LIBRARY_PATH="${HPL_LIBRARY_DIR}${LD_LIBRARY_PATH:+:${LD_LIBRARY_PATH}}"
        fi
        if ! command -v mpirun >/dev/null 2>&1; then
            echo "HPL MPI launcher is unavailable: mpirun"
            exit 1
        fi
        export OPENBLAS_NUM_THREADS="$HPL_THREADS_PER_PROCESS"
        export OMP_NUM_THREADS="$HPL_THREADS_PER_PROCESS"
        cd "$HPL_WORKDIR" || exit 1
        mpirun \
            -x OPENBLAS_NUM_THREADS \
            -x OMP_NUM_THREADS \
            -np "$HPL_MPI_PROCESSES" \
            "$HPL_EXECUTABLE"
        ;;
    hpcg)
        if [ "$#" -ne 0 ]; then exit 1; fi
        require_absolute_executable "HPCG" "$HPCG_EXECUTABLE"
        require_positive_integer "HPCG_MPI_PROCESSES" "$HPCG_MPI_PROCESSES"
        require_positive_integer "HPCG_THREADS_PER_PROCESS" "$HPCG_THREADS_PER_PROCESS"
        if [ ! -d "$HPCG_WORKDIR" ]; then
            echo "HPCG working directory is unavailable: $HPCG_WORKDIR"
            exit 1
        fi
        if ! command -v mpirun >/dev/null 2>&1; then
            echo "HPCG MPI launcher is unavailable: mpirun"
            exit 1
        fi
        export OMP_NUM_THREADS="$HPCG_THREADS_PER_PROCESS"
        export OMP_DYNAMIC=FALSE
        cd "$HPCG_WORKDIR" || exit 1
        mpirun \
            --map-by core \
            --bind-to core \
            -x OMP_NUM_THREADS \
            -x OMP_DYNAMIC \
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
