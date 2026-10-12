"""Syntax/identity contract checks, not Docker or A3 hardware acceptance."""
from pathlib import Path
import re
import shutil
import subprocess
import unittest

ROOT = Path(__file__).resolve().parents[3]
FILES = ("docker/NPU_A3_MIGRATION_GUIDE.md", "docker/NPU_MODEL_SDC_GUIDE.md",
         "docker/NPU_MODEL_TEST_RESULT.md", "docker/README-npu.md")


class MigrationDocs(unittest.TestCase):
    def test_local_links_and_fences(self):
        for name in FILES:
            path = ROOT / name
            text = path.read_text(encoding="utf-8")
            self.assertEqual(len(re.findall(r"^```[^\n]*$", text, re.M)) % 2, 0, name)
            for target in re.findall(r"\]\(([^)]+)\)", text):
                if "://" in target or target.startswith("#"):
                    continue
                self.assertTrue((path.parent / target.split("#")[0]).exists(), (name, target))

    @unittest.skipUnless(shutil.which("bash"), "Bash is unavailable; shell syntax remains unverified")
    def test_shell_syntax(self):
        for name in FILES:
            text = (ROOT / name).read_text(encoding="utf-8")
            for shell in re.findall(r"```bash\n(.*?)\n```", text, re.S):
                subprocess.run(["bash", "-n"], input=shell, text=True, check=True, capture_output=True)

    def test_a3_deployment_is_explicit_and_not_legacy(self):
        text = (ROOT / FILES[0]).read_text(encoding="utf-8")
        shells = "\n".join(re.findall(r"```bash\n(.*?)\n```", text, re.S))
        for forbidden in ("a2-portable-sizing.patch", "a2-cann83.patch", "a2-upstream-base.patch", "benchmark_check.sh", "cpu-runner.sock", "catmonitor-install", "catmonitor-stress-web", "29592", "fault26"):
            self.assertNotIn(forbidden, shells)
        for required in ("--enable-model-sdc", "model-memory-v1.patch", "--npu-chip-generation A3", "--npu-model-memory-mode checkpointed", "--env-file", "-addr=:19322", "--read-only", "--network none"):
            self.assertIn(required, shells)
        self.assertEqual(len(re.findall(r"docker run -d --name ", shells)), 4)
        self.assertIn("A3、新模型入口和新增激活重计算模式的完整硬件验收均为 **PENDING**", text)


if __name__ == "__main__":
    unittest.main()
