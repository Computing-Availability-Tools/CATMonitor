"""
SDMA Jump operation for NPU power jump testing.

This module provides SDMA-based memory copy operations with sleep intervals
for testing power jump scenarios.
"""

import torch
from . import custom_ops_lib


def sdma_jump(tensor: torch.Tensor, loop_num: int, sleep_ms: int) -> None:
    """
    Execute SDMA jump operation on NPU with sleep after copy.

    This function performs SDMA memory copies, synchronizes the stream,
    then sleeps for the specified duration to generate power jump scenarios.

    Args:
        tensor: A contiguous tensor on NPU device. Must be at least 200MB.
        loop_num: Number of SDMA copy iterations.
        sleep_ms: Sleep time in milliseconds after copy burst.

    Example:
        >>> x = torch.randn(256 * 1024 * 1024 // 2, dtype=torch.float16).npu()  # 256MB
        >>> sdma_jump(x, loop_num=100, sleep_ms=1000)
    """
    custom_ops_lib.sdma_jump(tensor, loop_num, sleep_ms)
