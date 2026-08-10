#!/usr/bin/env python3
# -*- coding: utf-8 -*-
# Copyright 2026 Huawei Technologies Co., Ltd
import pytest
import threading
from unittest.mock import MagicMock, patch

from common.thread import OpThread, stream_sync, stream_sync_timeout


@pytest.mark.unit
class TestOpThreadInit:

    def test_default_attributes(self):
        mock_stream = MagicMock()
        mock_check_stream = MagicMock()
        thread = OpThread(
            check_stream=mock_check_stream,
            stream=mock_stream,
            run_count=100,
            device=0,
            op_func=MagicMock(),
            params=[],
        )
        assert thread.run_count == 100
        assert thread.device == 0
        assert thread.result == []
        assert thread.check_stream is mock_check_stream
        assert thread.stream is mock_stream


@pytest.mark.unit
class TestStreamSync:

    def test_stream_sync_calls_synchronize(self):
        mock_stream = MagicMock()
        stream_sync(mock_stream)
        mock_stream.synchronize.assert_called_once()


@pytest.mark.unit
class TestStreamSyncTimeout:

    def test_fast_sync_returns_false(self):
        mock_stream = MagicMock()
        result = stream_sync_timeout(mock_stream, timeout=5)
        assert result is False

    def test_env_var_set(self):
        mock_stream = MagicMock()
        with patch.dict("os.environ", {}, clear=True):
            stream_sync_timeout(mock_stream, timeout=300)
            assert "ACL_DEVICE_SYNC_TIMEOUT" in __import__("os").environ
