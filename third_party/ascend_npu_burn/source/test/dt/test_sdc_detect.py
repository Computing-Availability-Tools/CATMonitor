#!/usr/bin/env python3
# -*- coding: utf-8 -*-
# Copyright 2026 Huawei Technologies Co., Ltd
import pytest
import torch

from detect.sdc.sdc_detect import SDCDetect
from detect.detect_base import DetectFactory


@pytest.mark.unit
class TestSDCDetect:

    def test_sdc_detect_identical_tensors(self):
        sdc_detect = SDCDetect()
        tensor_a = torch.randn(100, 100)
        tensor_b = tensor_a.clone()
        result = sdc_detect.core_detect(tensor_b, tensor_a)
        assert result == False


@pytest.mark.unit
class TestDetectFactory:

    def test_detect_factory_registration(self):
        detect_instances = DetectFactory.get_detect_instances("sdc")
        assert len(detect_instances) > 0
        for instance in detect_instances:
            assert isinstance(instance, SDCDetect)

    def test_detect_factory_get_instances(self):
        detect_instances = DetectFactory.get_detect_instances("sdc", ["sdc"])
        assert len(detect_instances) > 0
        for instance in detect_instances:
            assert hasattr(instance, 'core_detect')

    def test_detect_factory_invalid_type(self):
        detect_instances = DetectFactory.get_detect_instances("invalid_type")
        assert len(detect_instances) == 0


@pytest.mark.unit
@pytest.mark.npu
class TestSDCDetectOnNPU:
    """NPU上的SDC检测测试"""

    def test_sdc_detect_on_npu_identical(self, npu_device, clean_memory):
        """验证NPU上相同张量的SDC检测"""
        sdc_detect = SDCDetect()
        tensor_a = torch.randn(100, 100, device=f"npu:{npu_device}")
        tensor_b = tensor_a.clone()
        result = sdc_detect.core_detect(tensor_b, tensor_a)
        assert result == False, "Identical NPU tensors should return False"

    def test_sdc_detect_on_npu_difference(self, npu_device, clean_memory):
        """验证NPU上不同张量的SDC检测"""
        sdc_detect = SDCDetect()
        tensor_a = torch.randn(100, 100, device=f"npu:{npu_device}")
        tensor_b = tensor_a.clone()
        tensor_b[50, 50] += 0.001
        result = sdc_detect.core_detect(tensor_b, tensor_a)
        assert result == True, "NPU tensor difference should be detected"

    @pytest.mark.parametrize("dtype", [torch.float16, torch.float32, torch.bfloat16])
    def test_sdc_detect_different_dtypes_on_npu(self, npu_device, dtype, clean_memory):
        """验证NPU上不同数据类型的SDC检测"""
        sdc_detect = SDCDetect()
        tensor_a = torch.randn(100, 100, dtype=dtype, device=f"npu:{npu_device}")
        tensor_b = tensor_a.clone()
        tensor_b[50, 50] += 0.01
        result = sdc_detect.core_detect(tensor_b, tensor_a)
        assert result == True, f"Difference should be detected for dtype {dtype}"
