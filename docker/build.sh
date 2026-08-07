#!/bin/sh
set -e

MODE=${1:-auto}

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
        echo "Building NPU image (CGo + dcmi)..."
        if [ ! -d /usr/local/Ascend/driver ]; then
            echo "WARNING: /usr/local/Ascend/driver not found on build host."
            echo "         CGo headers/libraries may be missing."
            echo "         Ensure the driver package is installed before building."
        fi
        docker build \
            --build-arg ASCEND_DRIVER_PATH=/usr/local/Ascend/driver \
            -f docker/Dockerfile.npu \
            -t catmonitor-npu \
            .
        echo "Image: catmonitor-npu"
        ;;
    generic)
        echo "Building generic image (pure Go)..."
        docker build \
            -f docker/Dockerfile.generic \
            -t catmonitor-generic \
            .
        echo "Image: catmonitor-generic"
        ;;
    *)
        echo "Usage: $0 [auto|npu|generic]"
        echo "  auto    - detect NPU driver automatically (default)"
        echo "  npu     - build NPU image (CGo + dcmi tag)"
        echo "  generic - build generic image (pure Go, no NPU support)"
        exit 1
        ;;
esac
