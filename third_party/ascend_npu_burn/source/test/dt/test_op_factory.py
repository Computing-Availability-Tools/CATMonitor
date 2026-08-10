#!/usr/bin/env python3
# -*- coding: utf-8 -*-
# Copyright 2026 Huawei Technologies Co., Ltd
import pytest

from benchmarks.op.op_base import OpBase, OpFactory


@pytest.mark.unit
class TestOpBase:

    def test_default_attributes(self):
        op = OpBase()
        assert op.detect_type == ""
        assert op.sub_detect_types == []
        assert op.device is None
        assert op.task_config is None

    def test_set_sub_detect_types_default(self):
        op = OpBase()
        op.set_sub_detect_types()
        assert op.sub_detect_types == []

    def test_run_default(self):
        op = OpBase()
        result = op.run()
        assert result is None


@pytest.mark.unit
class TestOpFactory:

    def test_subclass_auto_registration(self):
        class CustomOp(OpBase):
            pass

        instance = OpFactory.get_op_instance("custom")
        assert instance is not None
        assert isinstance(instance, CustomOp)

    def test_get_nonexistent_op(self):
        instance = OpFactory.get_op_instance("nonexistent_op_12345")
        assert instance is None

    def test_registered_ops_are_new_instances(self):
        class NewOp(OpBase):
            pass

        inst1 = OpFactory.get_op_instance("new")
        inst2 = OpFactory.get_op_instance("new")
        assert inst1 is not inst2

    def test_underscore_stripped_in_lookup(self):
        class WithUnderscoreOp(OpBase):
            pass

        instance = OpFactory.get_op_instance("with_underscore")
        assert instance is not None
        assert isinstance(instance, WithUnderscoreOp)


@pytest.mark.unit
class TestConcreteOps:

    def test_conv2d_op_registered(self):
        from benchmarks.op.conv2d import Conv2dOp
        instance = OpFactory.get_op_instance("conv2d")
        assert instance is not None
        assert isinstance(instance, Conv2dOp)