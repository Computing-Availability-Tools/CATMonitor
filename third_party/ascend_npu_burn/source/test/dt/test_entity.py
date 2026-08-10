#!/usr/bin/env python3
# -*- coding: utf-8 -*-
# Copyright 2026 Huawei Technologies Co., Ltd
import pytest

from entity.context import TaskCtx, DataInfo


@pytest.mark.unit
class TestTaskCtx:

    def test_basic_creation(self):
        ctx = TaskCtx(task_id=1)
        assert ctx.task_id == 1
        assert ctx.task_args == {}
        assert ctx.task_result == {}

    def test_set_task_args(self):
        ctx = TaskCtx(task_id=2)
        ctx.task_args = {"dtype": "float32", "shape": [1024, 1024]}
        assert ctx.task_args["dtype"] == "float32"

    def test_set_task_result(self):
        ctx = TaskCtx(task_id=3)
        ctx.task_result = {"status": "pass", "err_count": 0}
        assert ctx.task_result["status"] == "pass"


@pytest.mark.unit
class TestDataInfo:

    def test_basic_creation(self):
        info = DataInfo(rank=0, dtype="float32", pattern="gauss_random")
        assert info.rank == 0
        assert info.dtype == "float32"
        assert info.pattern == "gauss_random"
        assert info.data is None
