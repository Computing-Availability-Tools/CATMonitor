#!/bin/bash
# -*- coding: utf-8 -*-
# Copyright 2026 Huawei Technologies Co., Ltd
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# ==============================================================================
# Ascend NPU Burn - 单元测试运行脚本
#
# 使用方法：
#   bash run_test.sh              # 运行全部单元测试 + 覆盖率
#   bash run_test.sh -v           # 详细输出
# ==============================================================================

set -e

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
PROJECT_ROOT="$SCRIPT_DIR/.."
DT_DIR="$SCRIPT_DIR/dt"

VERBOSE=""

while [[ $# -gt 0 ]]; do
    case $1 in
        -v|--verbose)
            VERBOSE="-v"
            shift
            ;;
        *)
            shift
            ;;
    esac
done

cd "$PROJECT_ROOT"

PYTEST_ARGS=(
    "$DT_DIR"
    "-m" "unit"
    "--cov=ascend_npu_burn"
    "--cov-report=term-missing"
    "--cov-report=lcov:test/output/lcov.info"
    "--cov-report=html:test/output/htmlcov"
)

if [ -n "$VERBOSE" ]; then
    PYTEST_ARGS+=("$VERBOSE")
fi

mkdir -p test/output

echo "=========================================="
echo "Ascend NPU Burn - Unit Tests"
echo "=========================================="
echo "Project Root: $PROJECT_ROOT"
echo "Test Dir:     $DT_DIR"
echo "=========================================="
echo ""

python3 -m pytest "${PYTEST_ARGS[@]}"

echo ""
echo "=========================================="
echo "✅ Tests completed"
echo "=========================================="
echo ""
echo "Coverage reports:"
echo "  - Terminal:  above"
echo "  - LCOV:      test/output/lcov.info"
echo "  - HTML:      test/output/htmlcov/index.html"
