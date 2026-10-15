"""Syntax/identity contract checks, not Docker or A3 hardware acceptance."""
from pathlib import Path
import re
import shutil
import subprocess
import unittest

ROOT = Path(__file__).resolve().parents[3]
FILES = ("docker/NPU_A3_MIGRATION_GUIDE.md", "docker/NPU_MODEL_SDC_GUIDE.md",
         "docker/NPU_MODEL_TEST_RESULT.md", "docker/README-npu.md",
         "docker/STRESS_JOB_RELEASE_GUIDE.md")


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

    def test_current_a3_patch_chain_and_source_are_consistent(self):
        names = ["portable-runtime-v1.patch", "model-sdc-v1.patch",
                 "model-memory-v1.patch", "job-parameters-v1.patch"]
        for name in (FILES[0], FILES[-1]):
            text = (ROOT / name).read_text(encoding="utf-8")
            shells = "\n".join(re.findall(r"```bash\n(.*?)\n```", text, re.S))
            self.assertIn("codex/stress-job-parameters", shells)
            self.assertNotIn("codex/npu-model-a3-migration", shells)
            self.assertIn("--compat-profile portable-model-sdc-memory-job-v1", shells)
            self.assertEqual(re.findall(r"--patch scripts/stress/patches/ascend_npu_burn/(\S+)", shells), names)
            for old in ("a2-upstream-base.patch", "a2-cann83.patch", "a2-portable-sizing.patch"):
                self.assertNotIn(old, shells)

    def test_full_candidate_manual_contract_and_real_parameter_flags(self):
        text = (ROOT / FILES[-1]).read_text(encoding="utf-8")
        shells = "\n".join(re.findall(r"```bash\n(.*?)\n```", text, re.S))
        self.assertEqual(len(re.findall(r"docker run -d --name ", shells)), 5)
        for value in ("cpu-workload.env", "npu-workload.env", "--read-only", "--network none",
                      "--require-pass", "--npu-operator-parameters", "--hpcg-runtime-seconds",
                      "--stream-threads", "--hpl-mpi-processes", "-addr=:19322", "set -euo pipefail"):
            self.assertIn(value, shells)
        self.assertNotIn("29592", shells)
        self.assertIn("**PENDING**", text)

    def test_a2_first_delivery_keeps_distinct_stack_and_hardware_boundary(self):
        text = (ROOT / FILES[-1]).read_text(encoding="utf-8")
        self.assertIn("export NPU_CHIP_GENERATION=A2", text)
        self.assertIn("A2 优先只拉 A2 三件套，不需要下载 A3 镜像", text)
        self.assertIn("a2-upstream-model-sdc-memory-job-v1", text)
        self.assertIn("/usr/local/Ascend/ascend-toolkit/set_env.sh", text)
        self.assertIn("2.9.0.post1+gitee7ba04", text)
        self.assertIn("本批镜像构建提交固定为 `4630b98227a31fc56a679c4785e35abd98d05d4e`", text)
        self.assertIn("不是“全新节点从零下载”的证据", text)
        self.assertIn("完整模型训练或隔夜可靠性认证", text)
        for package in ("catmonitor-npu", "catmonitor-stress-cpu", "catmonitor-stress-npu"):
            self.assertRegex(text, r"docker pull ghcr\.io/spike677/" + package + r"@sha256:[0-9a-f]{64}(?=\s|$)")
        for digest in re.findall(r"\bsha256:([0-9a-f]+)", text):
            self.assertEqual(len(digest), 64, "Invalid digest or Image ID length")


if __name__ == "__main__":
    unittest.main()
