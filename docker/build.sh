#!/bin/sh
set -e

MODE=${1:-auto}
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
PROJECT_ROOT=$(cd "$SCRIPT_DIR/.." && pwd)

# Auto-detect: check if Ascend driver is present
if [ "$MODE" = "auto" ]; then
    if [ -d /usr/local/Ascend/driver ]; then
        MODE=npu
    else
        MODE=generic
    fi
    echo "Auto-detected: $MODE"
fi

case "$MODE" in
    npu)
        echo "=== Building NPU image (two-step: compile + package) ==="

        DRIVER_PATH=/usr/local/Ascend/driver
        if [ ! -d "$DRIVER_PATH" ]; then
            echo "ERROR: $DRIVER_PATH not found on host."
            echo "       Install Ascend driver before building."
            exit 1
        fi

        echo "Step 1/2: Compiling binaries in golang container with driver mounted..."
        docker run --rm \
            -v "$DRIVER_PATH:/usr/local/Ascend/driver:ro" \
            -v "$PROJECT_ROOT:/app" \
            -w /app \
            -e CGO_ENABLED=1 \
            -e CGO_CFLAGS="-I/usr/local/Ascend/driver/include -w" \
            -e CGO_LDFLAGS="-L/usr/local/Ascend/driver/lib64/driver -ldcmi" \
            -e GOPROXY=https://goproxy.cn,direct \
            golang:1.23 \
            sh -c 'go build -tags dcmi -o catmonitor ./cmd/catmonitor && \
                   go build -o dfee ./features/dfee && \
                   go build -o web ./features/web && \
                   echo "Compile done."'

        echo "Step 2/2: Building runtime image (debian/glibc)..."
        docker build \
            -f docker/Dockerfile.npu \
            -t catmonitor-npu \
            "$PROJECT_ROOT"

        # Clean up compiled binaries (they're in the build context, not needed locally)
        rm -f "$PROJECT_ROOT/catmonitor" "$PROJECT_ROOT/dfee" "$PROJECT_ROOT/web"

        echo "Done. Image: catmonitor-npu"
        ;;

    generic)
        echo "=== Building generic image (multi-stage, pure Go) ==="
        docker build \
            -f docker/Dockerfile.generic \
            -t catmonitor-generic \
            "$PROJECT_ROOT"
        echo "Done. Image: catmonitor-generic"
        ;;

    *)
        echo "Usage: $0 [auto|npu|generic]"
        echo "  auto    - detect NPU driver automatically (default)"
        echo "  npu     - build NPU image (two-step: host-driver compile + runtime package)"
        echo "  generic - build generic image (multi-stage, no NPU support)"
        exit 1
        ;;
esac
