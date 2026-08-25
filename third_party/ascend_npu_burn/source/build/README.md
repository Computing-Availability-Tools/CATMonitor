# Ascend NPU Burn - 构建和安装指南

## 概述

本目录提供统一的构建和安装脚本，将以下内容打包成一个完整的 whl 包：

1. **PyTorch C++ 扩展** (NpuExtension，编译为 `.so`)
2. **ascend_npu_burn 代码** (完整的 Python 包)

所有组件整合为一个 whl 包，安装时一条命令即可。

## 使用方法

### 1. 编译

```bash
cd build
bash build.sh
```

**编译过程：**
1. 编译 C++ 扩展（sdma_burst, sdma_jump, npu_gmm）
2. 打包成 `ascend_npu_burn-*.whl`

### 2. 安装

```bash
pip3 install dist/ascend_npu_burn-*.whl
```

## 验证安装

### 1. 验证 C++ 扩展

```python
import torch
import torch_npu
from ascend_npu_burn.custom_ops import sdma_burst

x = torch.randn(1024, 1024, dtype=torch.float16).npu()
sdma_burst(x, loop_num=1000)
print("C++ extension works!")
```

### 2. 验证 ascend_npu_burn

```bash
npu-burn -h
```
