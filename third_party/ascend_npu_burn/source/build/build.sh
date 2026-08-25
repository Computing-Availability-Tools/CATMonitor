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
# Ascend NPU Burn - 构建脚本
#
# 使用方法：
#   bash build.sh
#
# 说明：
#   此脚本编译 ascend_npu_burn 包（含 C++ 扩展），生成 whl 分发包
# ==============================================================================

set -e

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
PROJECT_ROOT=$(dirname "$SCRIPT_DIR")

echo "=========================================="
echo "Ascend NPU Burn - Build Script"
echo "Project Root: $PROJECT_ROOT"
echo "=========================================="

echo ""
echo "Copying setup files to project root..."
cp "$SCRIPT_DIR/setup.py" "$PROJECT_ROOT/setup.py"

echo ""
echo "Building wheel package..."
cd "$PROJECT_ROOT"
python3 setup.py bdist_wheel

echo ""
echo "Cleaning up..."
rm -f "$PROJECT_ROOT/setup.py"

echo ""
echo "Moving package to build/dist..."
mkdir -p "$SCRIPT_DIR/dist"
mv "$PROJECT_ROOT/dist/"*.whl "$SCRIPT_DIR/dist/" 2>/dev/null || true
rm -rf "$PROJECT_ROOT/dist"

echo ""
echo "=========================================="
echo "Build completed successfully!"
echo "=========================================="
echo ""
echo "Generated files in: $SCRIPT_DIR/dist/"
ls -lh "$SCRIPT_DIR/dist/"
echo ""
echo "To install:"
echo "  pip3 install $SCRIPT_DIR/dist/ascend_npu_burn-*.whl"
echo ""
