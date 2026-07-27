#!/bin/bash
# Original benchmark dispatcher retained for deployment compatibility.

# STREAM is deployed and tuned per host. Keep the executable's absolute path,
# NUMA command and environment settings here rather than accepting them from
# YAML or the Web/CLI request.
STREAM_EXECUTABLE="/root/haoran/stream_omp"
export OMP_NUM_THREADS="${OMP_NUM_THREADS:-32}"

# HPL is the validated single-node Kunpeng 920 deployment. HPL.dat must remain
# beside xhpl because HPL reads it from the working directory. Do not move
# these host-specific paths, thread counts or MPI arguments into YAML.
HPL_WORKDIR="/root/haoran/hpl-2.3/bin/MyConfig"
HPL_EXECUTABLE="/root/haoran/hpl-2.3/bin/MyConfig/xhpl"
HPL_INPUT="/root/haoran/hpl-2.3/bin/MyConfig/HPL.dat"
HPL_OPENBLAS_LIB="/usr/local/openblas/lib"
HPL_MPI_PROCESSES=8
HPL_THREADS_PER_PROCESS=12

# HPCG is the validated official 3.1 reference build for the same 96-core
# Kunpeng 920 host. One MPI rank is bound to each physical core; the production
# command intentionally omits --report-bindings to keep captured output small.
HPCG_WORKDIR="/root/haoran/hpcg-3.1/build_Kunpeng_MPI_OMP/bin"
HPCG_EXECUTABLE="/root/haoran/hpcg-3.1/build_Kunpeng_MPI_OMP/bin/xhpcg"
HPCG_MPI_PROCESSES=96
HPCG_THREADS_PER_PROCESS=1

if [ $# -lt 1 ]; then
   echo "Insufficient number of parameters."
   exit 1
fi
benchmark_type=$1
shift

case "$benchmark_type" in
    hpl)
        if [ $# -ne 0 ]; then exit 1; fi
        if [ ! -x "$HPL_EXECUTABLE" ]; then
            echo "HPL executable is unavailable: $HPL_EXECUTABLE"
            exit 1
        fi
        if [ ! -r "$HPL_INPUT" ]; then
            echo "HPL input file is unavailable: $HPL_INPUT"
            exit 1
        fi
        if [ ! -d "$HPL_OPENBLAS_LIB" ]; then
            echo "OpenBLAS library directory is unavailable: $HPL_OPENBLAS_LIB"
            exit 1
        fi
        if ! command -v mpirun >/dev/null 2>&1; then
            echo "HPL MPI launcher is unavailable: mpirun"
            exit 1
        fi

        export LD_LIBRARY_PATH="${HPL_OPENBLAS_LIB}${LD_LIBRARY_PATH:+:${LD_LIBRARY_PATH}}"
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
        if [ $# -ne 0 ]; then exit 1; fi
        if [ ! -x "$HPCG_EXECUTABLE" ]; then
            echo "HPCG executable is unavailable: $HPCG_EXECUTABLE"
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
            --nx=32 \
            --ny=32 \
            --nz=32 \
            --rt=60
        ;;
    stream)
        if [ $# -ne 0 ]; then exit 1; fi
        if [ ! -x "$STREAM_EXECUTABLE" ]; then
            echo "STREAM executable is unavailable: $STREAM_EXECUTABLE"
            exit 1
        fi
        output=$(numactl --interleave=all "$STREAM_EXECUTABLE")
        status=$?
        if [ "$status" -ne 0 ]; then
            exit "$status"
        fi
        echo "$output"
        ;;
    *)
        echo "Unknown parameter."
        exit 1
        ;;
esac
