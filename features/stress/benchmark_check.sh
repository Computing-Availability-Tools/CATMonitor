#!/bin/bash
# Original benchmark dispatcher retained for deployment compatibility.

# STREAM is deployed and tuned per host. Keep the executable's absolute path,
# NUMA command and environment settings here rather than accepting them from
# YAML or the Web/CLI request.
STREAM_EXECUTABLE="/root/haoran/stream_omp"
export OMP_NUM_THREADS="${OMP_NUM_THREADS:-32}"

if [ $# -lt 2 ]; then
   echo "Insufficient number of parameters."
   exit 1
fi
benchmark_type=$1
path=$2
shift 2

case "$benchmark_type" in
    hpl)
        if [ $# -ne 0 ]; then exit 1; fi
        cd "$path"
        mpirun --allow-run-as-root --oversubscribe -x OMP_NUM_THREADS=32 --map-by ppr:16:node:pe=32 -x PATH -x LD_LIBRARY_PATH -x UCX_TLS=self,sm -mca pml ucx -mca btl ^vader,tcp,openib,uct ./xhpl
        ;;
    hpcg)
        if [ $# -ne 0 ]; then exit 1; fi
        mpirun --allow-run-as-root -x LD_LIBRARY_PATH -x PATH -x PWD -map-by ppr:608:node:pe=1 -mca pml ucx -mca btl ^vader,tcp,openib,uct -mca io romio321 "$path"/xhpcg
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
    osu)
        if [ $# -ne 3 ]; then exit 1; fi
        count_num=$1
        nic_port=$2
        hostfile=$3
        mpirun --allow-run-as-root -np $count_num -N 1 -x UCX_NET_DEVICES=$nic_port $hostfile $path/osu_alltoall -f -i 1 -m :1
        ;;
    *)
        echo "Unknown parameter."
        exit 1
        ;;
esac
