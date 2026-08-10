/*
 * -------------------------------------------------------------------------
 * This file is part of the MindCluster-AscendNPUBurn project.
 * Copyright (c) 2026 Huawei Technologies Co.,Ltd.
 *
 * MindCluster-AscendNPUBurn is licensed under Mulan PSL v2.
 * You can use this software according to the terms and conditions of the Mulan PSL v2.
 * You may obtain a copy of Mulan PSL v2 at:
 *
 *          http://license.coscl.org.cn/MulanPSL2
 *
 * THIS SOFTWARE IS PROVIDED ON AN "AS IS" BASIS, WITHOUT WARRANTIES OF ANY KIND,
 * EITHER EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO NON-INFRINGEMENT,
 * MERCHANTABILITY OR FIT FOR A PARTICULAR PURPOSE.
 * See the Mulan PSL v2 for more details.
 * -------------------------------------------------------------------------
 */
#ifndef FUNCTION_H
#define FUNCTION_H

#include <ATen/ATen.h>

// SdmaBurst 算子
void sdma_burst(at::Tensor& tensor, int64_t loop_num);
void sync_with_timeout(int32_t timeout_ms);

// SdmaJump 算子
void sdma_jump(at::Tensor& tensor, int64_t loop_num, int64_t sleep_ms);

// npu_gmm 算子
std::vector<at::Tensor> npu_gmm_forward(const std::vector<at::Tensor>& x, const std::vector<at::Tensor>& weight,
                                        const std::vector<at::Tensor>& bias,
                                        c10::optional<std::vector<int64_t>> group_list,
                                        c10::optional<int64_t> group_type, c10::optional<int64_t> group_list_type);
std::tuple<std::vector<at::Tensor>, std::vector<at::Tensor>, std::vector<at::Tensor>> npu_gmm_backward(
    const std::vector<at::Tensor>& grad, const std::vector<at::Tensor>& x, const std::vector<at::Tensor>& weight,
    const c10::optional<std::vector<int64_t>> group_list, c10::optional<int64_t> group_list_type);

#endif  //  FUNCTION_H
