#!/usr/bin/env python3
# -*- coding: utf-8 -*-
# Copyright 2026 Huawei Technologies Co., Ltd
import pytest
import torch
from unittest.mock import MagicMock, patch

from common.data_load import DataLoad
from entity.context import DataInfo


@pytest.mark.unit
class TestDataLoadSingleton:

    def test_singleton(self):
        from common.data_load import data_load as dl1
        dl2 = DataLoad()
        assert dl1 is dl2

    def test_data_pool_initially_empty(self):
        dl = DataLoad()
        assert isinstance(dl.data_pool, dict)


@pytest.mark.unit
class TestDataLoadCreateTensor:

    def test_create_tensor_invalid_shape_raises(self):
        dl = DataLoad()
        with pytest.raises(ValueError, match="create_tensor params"):
            dl.create_tensor([], torch.float32, 0, "gauss_random")

    def test_create_tensor_invalid_dtype_raises(self):
        dl = DataLoad()
        with pytest.raises(ValueError, match="create_tensor params"):
            dl.create_tensor([10, 10], "", 0, "gauss_random")

    def test_create_tensor_invalid_device_raises(self):
        dl = DataLoad()
        with pytest.raises(ValueError, match="create_tensor params"):
            dl.create_tensor([10, 10], torch.float32, None, "gauss_random")

    def test_int_type_uses_uniform_pattern(self):
        dl = DataLoad()
        with patch.object(dl, '_get_data') as mock_get:
            mock_data = torch.randint(0, 100, (1000,), dtype=torch.int8)
            mock_get.return_value = mock_data
            result = dl.create_tensor([10, 10], torch.int8, 0, "gauss_random")
            mock_get.assert_called_once_with(0, torch.int8, "uniform_random")

    def test_empty_pattern_defaults_to_gauss(self):
        dl = DataLoad()
        with patch.object(dl, '_get_data') as mock_get:
            mock_data = torch.randn(1000)
            mock_get.return_value = mock_data
            result = dl.create_tensor([10, 10], torch.float32, 0, "")
            mock_get.assert_called_once_with(0, torch.float32, "gauss_random")

    def test_none_pattern_defaults_to_gauss(self):
        dl = DataLoad()
        with patch.object(dl, '_get_data') as mock_get:
            mock_data = torch.randn(1000)
            mock_get.return_value = mock_data
            result = dl.create_tensor([10, 10], torch.float32, 0, None)
            mock_get.assert_called_once_with(0, torch.float32, "gauss_random")


@pytest.mark.unit
class TestDataLoadGetData:

    def test_missing_data_triggers_update(self):
        dl = DataLoad()
        dl.data_pool = {}
        with patch.object(dl, '_update_data') as mock_update:
            mock_update.side_effect = lambda did, dt, pat: dl.data_pool.update(
                {did: DataInfo(did, dt, pat, torch.randn(100))})
            result = dl._get_data(0, torch.float32, "gauss_random")
            mock_update.assert_called_once_with(0, torch.float32, "gauss_random")

    def test_existing_data_returned_directly(self):
        dl = DataLoad()
        existing_data = torch.randn(100)
        dl.data_pool = {0: DataInfo(0, torch.float32, "gauss_random", existing_data)}
        with patch.object(dl, '_update_data') as mock_update:
            result = dl._get_data(0, torch.float32, "gauss_random")
            mock_update.assert_not_called()
            assert result is existing_data

    def test_dtype_mismatch_triggers_update(self):
        dl = DataLoad()
        dl.data_pool = {0: DataInfo(0, torch.float32, "gauss_random", torch.randn(100))}
        with patch.object(dl, '_update_data') as mock_update:
            mock_update.side_effect = lambda did, dt, pat: dl.data_pool.update(
                {did: DataInfo(did, dt, pat, torch.randn(100, dtype=torch.float16))})
            result = dl._get_data(0, torch.float16, "gauss_random")
            mock_update.assert_called_once()

    def test_pattern_mismatch_triggers_update(self):
        dl = DataLoad()
        dl.data_pool = {0: DataInfo(0, torch.float32, "gauss_random", torch.randn(100))}
        with patch.object(dl, '_update_data') as mock_update:
            mock_update.side_effect = lambda did, dt, pat: dl.data_pool.update(
                {did: DataInfo(did, dt, pat, torch.randn(100))})
            result = dl._get_data(0, torch.float32, "uniform_random")
            mock_update.assert_called_once()
