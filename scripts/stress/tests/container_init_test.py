"""Real Linux process tests; no Docker daemon, privileged namespace or NPU."""
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
import unittest

HELPER = Path(__file__).resolve().parents[3] / "docker/stress/container_init.py"


@unittest.skipUnless(sys.platform.startswith("linux"), "Linux process lifecycle NOT_RUN")
class ContainerInitTest(unittest.TestCase):
    def start(self, *command):
        proc = subprocess.Popen([sys.executable, str(HELPER), "--", *command],
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)

        def cleanup():
            if proc.poll() is None:
                proc.terminate()
            proc.communicate(timeout=8)

        self.addCleanup(cleanup)
        return proc

    def wait_file(self, path):
        deadline = time.monotonic() + 4
        while time.monotonic() < deadline:
            if path.exists() and path.read_text():
                return path.read_text()
            time.sleep(0.02)
        self.fail("child did not become ready")

    def test_original_entrypoint_arguments_and_exit_code(self):
        proc = self.start(sys.executable, "-c", "import sys; print(sys.argv[1]); sys.exit(7)", "__serve__")
        out, _ = proc.communicate(timeout=4)
        self.assertEqual(out.strip(), "__serve__")
        self.assertEqual(proc.returncode, 7)

    def test_missing_entrypoint_fails_instead_of_staying_ready(self):
        proc = self.start("/nonexistent/catmonitor-init-test")
        _, err = proc.communicate(timeout=4)
        self.assertEqual(proc.returncode, 127)
        self.assertIn("container init:", err)

    def test_sigterm_is_forwarded_to_original_entrypoint(self):
        with tempfile.TemporaryDirectory() as folder:
            ready = Path(folder) / "ready"
            stopped = Path(folder) / "stopped"
            code = ("import pathlib,signal,sys,time\n"
                    "def stop(*args):\n pathlib.Path(sys.argv[2]).write_text('TERM'); sys.exit(0)\n"
                    "signal.signal(signal.SIGTERM,stop)\n"
                    "pathlib.Path(sys.argv[1]).write_text('ready')\n"
                    "while True: time.sleep(.1)\n")
            proc = self.start(sys.executable, "-c", code, str(ready), str(stopped))
            self.wait_file(ready)
            proc.send_signal(signal.SIGTERM)
            proc.communicate(timeout=4)
            self.assertEqual(proc.returncode, 0)
            self.assertEqual(stopped.read_text(), "TERM")

    def test_ignored_sigterm_is_bounded(self):
        with tempfile.TemporaryDirectory() as folder:
            ready = Path(folder) / "ready"
            code = ("import pathlib,signal,sys,time; signal.signal(signal.SIGTERM,signal.SIG_IGN); "
                    "pathlib.Path(sys.argv[1]).write_text('ready'); time.sleep(60)")
            proc = self.start(sys.executable, "-c", code, str(ready))
            self.wait_file(ready)
            proc.terminate()
            proc.communicate(timeout=8)
            self.assertEqual(proc.returncode, 128 + signal.SIGKILL)

    def test_adopted_orphan_is_reaped_while_entrypoint_keeps_running(self):
        with tempfile.TemporaryDirectory() as folder:
            pidfile = Path(folder) / "orphan"
            code = """import os,pathlib,sys,time
child=os.fork()
if child == 0:
    orphan=os.fork()
    if orphan == 0:
        time.sleep(.15)
        os._exit(0)
    pathlib.Path(sys.argv[1]).write_text(str(orphan))
    os._exit(0)
os.waitpid(child,0)
while True: time.sleep(.1)
"""
            proc = self.start(sys.executable, "-c", code, str(pidfile))
            orphan = int(self.wait_file(pidfile))
            deadline = time.monotonic() + 4
            while time.monotonic() < deadline and Path("/proc/%d" % orphan).exists():
                time.sleep(0.02)
            self.assertFalse(Path("/proc/%d" % orphan).exists(), "orphan left as a zombie")
            self.assertIsNone(proc.poll())


if __name__ == "__main__":
    unittest.main()
