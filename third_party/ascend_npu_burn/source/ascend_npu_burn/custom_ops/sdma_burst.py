"""
SDMA Burst operation for NPU power testing.

This module provides SDMA-based memory copy operations for stress testing
NPU power consumption.
"""

import torch
from . import custom_ops_lib


def sdma_burst(tensor: torch.Tensor, loop_num: int) -> None:
    """
    Execute SDMA burst operation on NPU.

    This function performs repeated SDMA memory copies to generate high
    memory bandwidth load for power testing purposes.

    Args:
        tensor: A contiguous tensor on NPU device. Must be at least 512 bytes.
        loop_num: Number of SDMA copy iterations to perform.

    Example:
        >>> x = torch.randn(1024, 1024, dtype=torch.float16).npu()
        >>> sdma_burst(x, loop_num=1000)
    """
    custom_ops_lib.sdma_burst(tensor, loop_num)


def sync_with_timeout(timeout_ms: int) -> None:
    """
    Synchronize NPU stream with timeout.

    This function waits for all operations on the current NPU stream to
    complete, with a specified timeout.

    Args:
        timeout_ms: Timeout in milliseconds. If operations don't complete
                   within this time, an exception is raised.

    Raises:
        RuntimeError: If synchronization times out or fails.

    Example:
        >>> sdma_burst(x, loop_num=1000)
        >>> sync_with_timeout(5000)  # Wait up to 5 seconds
    """
    custom_ops_lib.sync_with_timeout(timeout_ms)
