#!/bin/bash
set -e

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
cd "$SCRIPT_DIR"

source /usr/local/Ascend/cann-A2/ascend-toolkit/set_env.sh

BUILD_ALL=0
if [ "$1" = "all" ]; then
    BUILD_ALL=1
fi

ARCH=$(uname -m)
OUTPUT_DIR="$SCRIPT_DIR/dist"
mkdir -p "$OUTPUT_DIR"

echo "=========================================="
echo "CI Build Script - Ascend NPU Burn"
echo "Architecture: $ARCH"
if [ $BUILD_ALL -eq 1 ]; then
    echo "Build mode: ALL environments"
else
    echo "Build mode: Default (pt28py310_env only)"
fi
echo "=========================================="

eval "$(conda shell.bash hook 2>/dev/null)"

DEFAULT_ENV="pt28py310_env"

if [ $BUILD_ALL -eq 1 ]; then
    ENVS=$(conda env list 2>/dev/null | grep -E 'pt\S*py\S*_env' | awk '{print $1}' | sort -u)
else
    ENVS="$DEFAULT_ENV"
fi

if [ $BUILD_ALL -eq 1 ] && [ -z "$ENVS" ]; then
    echo "ERROR: No conda environments matching 'pt*py*_env' found!"
    conda env list
    exit 1
fi

if [ $BUILD_ALL -ne 1 ]; then
    if ! conda env list 2>/dev/null | grep -qE "^${DEFAULT_ENV}\s"; then
        echo "ERROR: Default environment '${DEFAULT_ENV}' not found!"
        conda env list
        exit 1
    fi
fi

echo "Target conda environment(s):"
echo "$ENVS"
echo ""

for ENV_NAME in $ENVS; do
    echo "=========================================="
    echo "Processing environment: $ENV_NAME"
    echo "=========================================="

    conda activate "$ENV_NAME" 2>/dev/null
    if [ $? -ne 0 ]; then
        echo "WARNING: Failed to activate $ENV_NAME, skipping..."
        continue
    fi

    PYTHON_VERSION=$(python3 -c "import sys; print(f'{sys.version_info.major}.{sys.version_info.minor}')")
    echo "Python version: $PYTHON_VERSION"

    PYTORCH_VERSION=$(pip3 list 2>/dev/null | grep -iE "^torch\s+" | awk '{print $2}')
    if [ -z "$PYTORCH_VERSION" ]; then
        echo "WARNING: PyTorch not found in $ENV_NAME, skipping..."
        conda deactivate
        continue
    fi
    PYTORCH_VERSION=$(echo "$PYTORCH_VERSION" | sed 's/+.*//')
    echo "PyTorch version: $PYTORCH_VERSION"

    rm -rf build/dist/

    OUTPUT_BAK=""
    if [ -d "$OUTPUT_DIR" ]; then
        OUTPUT_BAK="${OUTPUT_DIR}_bak_$$"
        mv "$OUTPUT_DIR" "$OUTPUT_BAK"
    fi

    echo "Running build..."
    bash build/build.sh

    if [ -n "$OUTPUT_BAK" ]; then
        mv "$OUTPUT_BAK" "$OUTPUT_DIR"
    fi

    if [ -d "build/dist" ] && ls build/dist/*.whl 1>/dev/null 2>&1; then
        mkdir -p "$OUTPUT_DIR"
        for WHL_FILE in build/dist/*.whl; do
            BASENAME=$(basename "$WHL_FILE")
            cp "$WHL_FILE" "$OUTPUT_DIR/$BASENAME"
            echo "Copied: $BASENAME"
        done
    else
        echo "WARNING: No whl found in build/dist/ for $ENV_NAME"
    fi

    conda deactivate
    echo ""
done

echo "=========================================="
echo "CI Build Completed!"
echo "=========================================="
echo "Generated packages:"
ls -lh "$OUTPUT_DIR"/*.whl 2>/dev/null || echo "No packages found."
