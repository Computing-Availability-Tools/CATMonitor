#!/usr/bin/env python3
# -*- coding: utf-8 -*-
# Copyright 2026 Huawei Technologies Co., Ltd
import pytest
import json

from common.utils import TestCase, ConfigManager, EngineConfig


@pytest.mark.unit
class TestTestCase:

    def test_basic_creation(self):
        case = TestCase(name="matmul", data={"seq": 1, "dtype": "float32"})
        assert case.name == "matmul"
        assert case.seq == 1

    def test_getitem(self):
        case = TestCase(name="conv2d", data={"seq": 2, "dtype": "float16", "shape": [[1, 2], [2, 3]]})
        assert case["dtype"] == "float16"
        assert case["shape"] == [[1, 2], [2, 3]]

    def test_getitem_missing_key(self):
        case = TestCase(name="op", data={"seq": 1})
        assert case["nonexistent"] is None

    def test_repr(self):
        case = TestCase(name="matmul", data={"seq": 1, "dtype": "float32"})
        repr_str = repr(case)
        assert "matmul" in repr_str
        assert "seq=1" in repr_str


@pytest.mark.unit
class TestConfigManager:

    @pytest.fixture
    def sample_config(self):
        return {
            "cases": [
                {"matmul": {"seq": 1, "dtype": ["float32"], "shape": [[[1024, 1024], [1024, 1024]]]}},
                {"conv2d": {"seq": 2, "dtype": ["float16"], "shape": [[[1, 3, 224, 224], [64, 3, 7, 7]]]}},
            ],
            "group": {
                "all_ops": [1, 2],
                "matmul_only": [1],
            }
        }

    @pytest.fixture
    def config_file(self, sample_config, tmp_path):
        config_path = tmp_path / "test_config.json"
        with open(config_path, 'w', encoding='utf-8') as f:
            json.dump(sample_config, f)
        return str(config_path)

    def test_load_from_file(self, config_file):
        cfg = ConfigManager(config_file)
        assert cfg.get_by_seq(1) is not None

    def test_get_by_seq(self, sample_config):
        cfg = ConfigManager(sample_config)
        case = cfg.get_by_seq(1)
        assert case is not None
        assert case.name == "matmul"

    def test_get_by_seq_not_found(self, sample_config):
        cfg = ConfigManager(sample_config)
        assert cfg.get_by_seq(999) is None

    def test_get_by_name(self, sample_config):
        cfg = ConfigManager(sample_config)
        case = cfg.get_by_name("conv2d")
        assert case is not None
        assert case.seq == 2

    def test_get_by_name_not_found(self, sample_config):
        cfg = ConfigManager(sample_config)
        assert cfg.get_by_name("nonexistent") is None

    def test_get_by_group(self, sample_config):
        cfg = ConfigManager(sample_config)
        group = cfg.get_by_group("all_ops")
        assert len(group) == 2

    def test_get_by_group_single(self, sample_config):
        cfg = ConfigManager(sample_config)
        group = cfg.get_by_group("matmul_only")
        assert len(group) == 1
        assert group[0].name == "matmul"

    def test_get_by_group_not_found(self, sample_config):
        cfg = ConfigManager(sample_config)
        assert cfg.get_by_group("nonexistent") == []

    def test_getitem_by_int(self, sample_config):
        cfg = ConfigManager(sample_config)
        case = cfg[1]
        assert case is not None
        assert case.name == "matmul"

    def test_getitem_by_name(self, sample_config):
        cfg = ConfigManager(sample_config)
        case = cfg["conv2d"]
        assert case is not None
        assert case.seq == 2

    def test_getitem_by_group_name(self, sample_config):
        cfg = ConfigManager(sample_config)
        group = cfg["all_ops"]
        assert isinstance(group, list)
        assert len(group) == 2

    def test_getitem_invalid_key(self, sample_config):
        cfg = ConfigManager(sample_config)
        assert cfg[999] is None

    def test_empty_config(self):
        cfg = ConfigManager({})
        assert cfg.get_by_seq(1) is None
        assert cfg.get_by_name("matmul") is None
        assert cfg.get_by_group("all") == []


@pytest.mark.unit
class TestEngineConfig:

    def test_creation(self):
        config = EngineConfig(
            active_numa_ids=[0, 1],
            worker_resources={"queue": "test"},
            active_numa_map={0: [0, 1], 1: [2, 3]},
            numa_configs={0: [0, 1, 2, 3], 1: [4, 5, 6, 7]},
        )
        assert config.active_numa_ids == [0, 1]
        assert config.worker_resources == {"queue": "test"}
        assert config.active_numa_map == {0: [0, 1], 1: [2, 3]}
        assert config.numa_configs == {0: [0, 1, 2, 3], 1: [4, 5, 6, 7]}
