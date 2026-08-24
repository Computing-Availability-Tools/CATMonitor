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
#include <torch/extension.h>
#include <torch/library.h>

#include "function.h"



// 通过pybind将c++接口和python接口绑定
PYBIND11_MODULE(TORCH_EXTENSION_NAME, m)
{

    // SdmaBurst 算子
    m.def("sdma_burst", &sdma_burst, "SDMA burst operation for power testing");
    m.def("sync_with_timeout", &sync_with_timeout, "Synchronize NPU stream with timeout");

    // SdmaJump 算子
    m.def("sdma_jump", &sdma_jump, "SDMA jump operation for power jump testing");

    // 前向
    using gmm_forward = std::vector<at::Tensor> (*)(const std::vector<at::Tensor>&, const std::vector<at::Tensor>&,
                                                    const std::vector<at::Tensor>&, c10::optional<std::vector<int64_t>>,
                                                    c10::optional<int64_t>, c10::optional<int64_t>);
    m.def("npu_gmm_forward", (gmm_forward)&npu_gmm_forward, "Grouped matmul forward with group_list type List[int]");

    // 反向
    using gmm_backward = std::tuple<std::vector<at::Tensor>, std::vector<at::Tensor>, std::vector<at::Tensor>> (*)(
        const std::vector<at::Tensor>&, const std::vector<at::Tensor>&, const std::vector<at::Tensor>&,
        const c10::optional<std::vector<int64_t>>, c10::optional<int64_t>);
    m.def("npu_gmm_backward", (gmm_backward)&npu_gmm_backward,
          "Grouped matmul backward with group_list type List[int]");
}
