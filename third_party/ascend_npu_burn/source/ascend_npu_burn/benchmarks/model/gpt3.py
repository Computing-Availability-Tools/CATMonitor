#!/usr/bin/env python3
# -*- coding: utf-8 -*-
#
# This file is part of the MindCluster-AscendNPUBurn project.
# Copyright (c) 2026-2026 Huawei Technologies Co., Ltd. All Rights Reserved.
# MindCluster-AscendNPUBurn is licensed under Mulan PSL v2.
# You can use this software according to the terms and conditions of the Mulan PSL v2.
# You may obtain a copy of Mulan PSL v2 at:
#          http://license.coscl.org.cn/MulanPSL2
# THIS SOFTWARE IS PROVIDED ON AN "AS IS" BASIS, WITHOUT WARRANTIES OF ANY KIND,
# EITHER EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO NON-INFRINGEMENT,
# MERCHANTABILITY OR FIT FOR A PARTICULAR PURPOSE.
# See the Mulan PSL v2 for more details.
# ===========================================================================
from benchmarks.model.model_base import ModelBase
from benchmarks.model.model_base import build_training_head


class Gpt3Model(ModelBase):
    MODEL_KEY = "gpt3"

    def _create_model(self):
        import torch
        from transformers import GPT2Config, GPT2Model

        hidden_size = self._required_case_int("hidden_size")
        num_heads = self._required_case_int("num_attention_heads")
        num_layers = self._required_case_int("num_hidden_layers")
        inner_size = self._required_case_int("intermediate_size")
        vocab_size = self._required_case_int("vocab_size")
        class_count = self._required_case_int("num_classes")
        seq_len = self._required_case_int("seq_len")

        if hidden_size <= 0 or num_heads <= 0 or num_layers <= 0 or inner_size <= 0:
            raise ValueError("gpt3 shape arguments must be positive integers")
        if hidden_size % num_heads != 0:
            raise ValueError("hidden_size must be divisible by num_attention_heads")

        runtime_dtype = self._prepare_runtime(torch)
        model_shape = GPT2Config(
            vocab_size=vocab_size,
            n_positions=seq_len,
            n_ctx=seq_len,
            n_embd=hidden_size,
            n_layer=num_layers,
            n_head=num_heads,
            n_inner=inner_size,
            use_cache=False,
        )

        decoder = GPT2Model(model_shape)
        self._network = build_training_head(
            torch,
            decoder,
            model_shape.hidden_size,
            class_count,
        ).to(self._run_device)
        self._network = self._network.to(dtype=runtime_dtype)

        self._criterion = torch.nn.CrossEntropyLoss()
        self._prime_sample_bank(torch, model_shape.vocab_size, class_count)
