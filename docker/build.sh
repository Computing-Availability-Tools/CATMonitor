#!/bin/sh
set -e

MODE=${1:-auto}
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
PROJECT_ROOT=$(cd "$SCRIPT_DIR/.." && pwd)
BUILD_DIR="$PROJECT_ROOT/docker/.build"
DOCKER_BIN=${CATMONITOR_DOCKER_BIN:-docker}
DOCKER_BUILD_NETWORK=${CATMONITOR_DOCKER_BUILD_NETWORK:-default}

case "$DOCKER_BUILD_NETWORK" in
    default|host|none) ;;
    *)
        echo "ERROR: CATMONITOR_DOCKER_BUILD_NETWORK must be default, host, or none." >&2
        exit 1
        ;;
esac
# Forward only variables explicitly configured by the administrator. Values are
# inherited from the environment and are not printed or persisted in this file.
PROXY_BUILD_ARGS=
GO_BUILD_ARGS=
GO_RUN_ENV_ARGS=
for proxy_name in HTTP_PROXY HTTPS_PROXY NO_PROXY http_proxy https_proxy no_proxy; do
    eval "proxy_value=\${$proxy_name-}"
    if [ -n "$proxy_value" ]; then
        PROXY_BUILD_ARGS="$PROXY_BUILD_ARGS --build-arg $proxy_name"
        GO_RUN_ENV_ARGS="$GO_RUN_ENV_ARGS -e $proxy_name"
    fi
done
for go_name in GOPROXY GOSUMDB GOPRIVATE GONOSUMDB; do
    eval "go_value=\${$go_name-}"
    if [ -n "$go_value" ]; then
        GO_BUILD_ARGS="$GO_BUILD_ARGS --build-arg $go_name"
        GO_RUN_ENV_ARGS="$GO_RUN_ENV_ARGS -e $go_name"
    fi
done

if [ -n "$PROXY_BUILD_ARGS" ]; then
    echo "Docker build proxy: configured"
fi
if [ -n "$GO_BUILD_ARGS" ]; then
    echo "Go module environment: configured"
fi

cleanup_build_dir() {
    case "$BUILD_DIR" in
        "$PROJECT_ROOT"/docker/.build) rm -rf -- "$BUILD_DIR" ;;
        *)
            echo "ERROR: refusing to clean unexpected build directory: $BUILD_DIR" >&2
            exit 1
            ;;
    esac
}

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

        cleanup_build_dir
        mkdir -p "$BUILD_DIR"
        trap cleanup_build_dir EXIT HUP INT TERM

        echo "Step 1/2: Compiling binaries in golang container with driver mounted..."
        # Argument lists contain constant option/variable names only. Their
        # values are inherited by Docker and never expanded into this command.
        # shellcheck disable=SC2086
        "$DOCKER_BIN" run --rm --network "$DOCKER_BUILD_NETWORK" $GO_RUN_ENV_ARGS \
            -v "$DRIVER_PATH:/usr/local/Ascend/driver:ro" \
            -v "$PROJECT_ROOT:/app:ro" \
            -v "$BUILD_DIR:/out" \
            -w /app \
            -e CGO_ENABLED=1 \
            -e CGO_CFLAGS="-I/usr/local/Ascend/driver/include -w" \
            -e CGO_LDFLAGS="-L/usr/local/Ascend/driver/lib64/driver -ldcmi -Wl,--allow-shlib-undefined" \
            golang:1.23 \
            sh -c 'go build -tags dcmi -o /out/catmonitor ./cmd/catmonitor && \
                   CGO_ENABLED=0 go build -o /out/dfee ./features/dfee && \
                   CGO_ENABLED=0 go build -o /out/web ./features/web && \
                   CGO_ENABLED=0 go build -o /out/cpu-runner-client ./features/stress/cmd/cpu-runner-client && \
                   echo "Compile done."'

        echo "Step 2/2: Building runtime image (debian/glibc)..."
        # shellcheck disable=SC2086
        "$DOCKER_BIN" build --network "$DOCKER_BUILD_NETWORK" $PROXY_BUILD_ARGS \
            -f docker/Dockerfile.npu \
            -t catmonitor-npu \
            "$PROJECT_ROOT"

        echo "Done. Image: catmonitor-npu"
        ;;

    generic)
        echo "=== Building generic image (multi-stage, pure Go, Alpine) ==="
        # shellcheck disable=SC2086
        "$DOCKER_BIN" build --network "$DOCKER_BUILD_NETWORK" $PROXY_BUILD_ARGS $GO_BUILD_ARGS \
            -f docker/Dockerfile.generic \
            -t catmonitor-generic \
            "$PROJECT_ROOT"
        echo "Done. Image: catmonitor-generic"
        ;;

    gpu)
        echo "=== Building GPU image (multi-stage, pure Go, Debian/glibc) ==="
        # shellcheck disable=SC2086
        "$DOCKER_BIN" build --network "$DOCKER_BUILD_NETWORK" $PROXY_BUILD_ARGS $GO_BUILD_ARGS \
            -f docker/Dockerfile.gpu \
            -t catmonitor-gpu \
            "$PROJECT_ROOT"
        echo "Done. Image: catmonitor-gpu"
        ;;

    *)
        echo "Usage: $0 [auto|npu|generic|gpu]"
        echo "  auto    - detect NPU driver automatically (default)"
        echo "  npu     - build NPU image (two-step: host-driver compile + runtime package)"
        echo "  generic - build generic image (multi-stage, Alpine, no NPU support)"
        echo "  gpu     - build GPU image (multi-stage, Debian/glibc, for NVIDIA GPU)"
        exit 1
        ;;
esac
