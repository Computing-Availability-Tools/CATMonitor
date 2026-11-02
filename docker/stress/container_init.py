"""Legacy NPU container PID 1: reap orphans and forward shutdown signals.

This is not a workload runner. The original image entrypoint still initializes
Ascend and runs __serve__; jobs still arrive through Docker exec/Controller.
Only Python's standard library is required; the source is mounted read-only.
"""
import ctypes
import os
import signal
import sys
import time


def supervise(argv):
    # PID 1 adopts orphans automatically. Subreaper also makes the same
    # behavior testable without privileged PID-namespace creation.
    if os.getpid() != 1:
        libc = ctypes.CDLL(None, use_errno=True)
        libc.prctl.argtypes = [ctypes.c_int] + [ctypes.c_ulong] * 4
        libc.prctl.restype = ctypes.c_int
        if libc.prctl(36, 1, 0, 0, 0) != 0:  # PR_SET_CHILD_SUBREAPER
            raise OSError(ctypes.get_errno(), "cannot enable child subreaper")
    child = os.fork()
    if child == 0:
        try:
            os.setsid()
            for sig in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP, signal.SIGCHLD):
                signal.signal(sig, signal.SIG_DFL)
            os.execvp(argv[0], argv)
        except OSError as error:
            print("container init: " + str(error), file=sys.stderr, flush=True)
            os._exit(127)

    stopping = None
    killed = False

    def forward(sig):
        try:
            os.killpg(child, sig)
        except ProcessLookupError:
            try:
                os.kill(child, sig)
            except ProcessLookupError:
                pass

    def stop(sig, _frame):
        nonlocal stopping
        if stopping is None:
            stopping = time.monotonic()
        forward(sig)

    for sig in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
        signal.signal(sig, stop)
    signal.signal(signal.SIGCHLD, lambda _sig, _frame: None)
    primary_status = None
    while True:
        while True:
            try:
                pid, status = os.waitpid(-1, os.WNOHANG)
            except ChildProcessError:
                pid = 0
            except InterruptedError:
                continue
            if pid == 0:
                break
            if pid == child:
                primary_status = status
        if primary_status is not None:
            if os.WIFEXITED(primary_status):
                return os.WEXITSTATUS(primary_status)
            return 128 + os.WTERMSIG(primary_status)
        if stopping is not None and not killed and time.monotonic() - stopping >= 5:
            # Only the entrypoint's process group; never signal all host PIDs.
            forward(signal.SIGKILL)
            killed = True
        time.sleep(0.02)


if __name__ == "__main__":
    if len(sys.argv) < 3 or sys.argv[1] != "--":
        sys.exit("usage: container_init.py -- ENTRYPOINT [ARG ...]")
    sys.exit(supervise(sys.argv[2:]))
