/**
 * @file sdma_burst.cpp
 *
 * Copyright (C) 2024-2025. Huawei Technologies Co., Ltd. All rights reserved.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.
 */
#include <c10/core/DeviceGuard.h>
#include <pybind11/pybind11.h>
#include <torch/extension.h>

#include <algorithm>
#include <iostream>

#include "acl/acl.h"
#include "torch_npu/csrc/core/npu/NPUStream.h"

void sdma_burst(at::Tensor& tensor, int64_t loop_num)
{
    at::DeviceGuard guard(tensor.device());

    TORCH_CHECK(tensor.device().type() == c10::kPrivateUse1, "Tensor must be on NPU.");
    TORCH_CHECK(tensor.is_contiguous(), "Tensor must be contiguous.");

    void* base_ptr = tensor.data_ptr();
    size_t total_bytes = tensor.numel() * tensor.element_size();
    size_t max_buffer = 64ULL * 1024 * 1024;
    size_t buffer_size = std::min(total_bytes, max_buffer);

    TORCH_CHECK(buffer_size % 512 == 0, "Buffer size must be a multiple of 512 for 64-byte SDMA alignment.");

    size_t copy_size = (buffer_size * 7) / 8;
    size_t offset = buffer_size - copy_size;

    void* dst_ptr = base_ptr;
    void* src_ptr = static_cast<uint8_t*>(base_ptr) + offset;

    aclrtStream stream = c10_npu::getCurrentNPUStream().stream(false);

    pybind11::gil_scoped_release release;

    for (int64_t i = 0; i < loop_num; ++i)
    {
        aclrtMemcpyAsync(dst_ptr, buffer_size, src_ptr, copy_size, ACL_MEMCPY_DEVICE_TO_DEVICE, stream);
    }
}

void sync_with_timeout(int32_t timeout_ms)
{
    aclrtStream stream = c10_npu::getCurrentNPUStream().stream(false);
    pybind11::gil_scoped_release release;

    aclError ret = aclrtSynchronizeStreamWithTimeout(stream, timeout_ms);

    TORCH_CHECK(ret == ACL_SUCCESS, "Hardware Stream Synchronize Timeout or Failed! Error code: ", ret,
                ". Possible hardware hang due to extreme power draw.");
}
