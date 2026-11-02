"""Maintained documentation contracts, not Docker or hardware acceptance."""
from pathlib import Path
import re
import shutil
import subprocess
import unittest
from urllib.parse import unquote

ROOT = Path(__file__).resolve().parents[3]
DELIVERY = "docker/STRESS_JOB_RELEASE_GUIDE.md"
MODEL = "docker/NPU_MODEL_SDC_GUIDE.md"
DEVELOPER = "docker/DEVELOPER_GUIDE.md"
FILES = (
    "docker/README.md", "docker/README-generic.md", "docker/README-gpu.md",
    "docker/README-npu.md", DEVELOPER, DELIVERY, MODEL,
    "features/stress/README.md", "features/stress/STRESS_USER_GUIDE.md",
    "features/stress/STRESS_SPEC.md", "features/stress/STRESS_DESIGN.md",
    "features/stress/STRESS_TEST_GUIDE.md", "features/stress/OSS_RELEASE_AUDIT.md",
    "features/stress/THIRD_PARTY_NOTICES.md",
    "configs/stress/npu-model/README.md",
    "scripts/stress/patches/ascend_npu_burn/README.md",
)
RETIRED = (
    "DEMO_GUIDE.md", "NPU_A3_MIGRATION_GUIDE.md", "NPU_COMPAT_REVIEW.md",
    "NPU_MODEL_TEST_RESULT.md", "NPU_OPERATOR_GUIDE.md", "RESTRICTED_ENV_GUIDE.md",
)
# These are executable deployment/build/operation examples. Specification and
# test-plan snippets may intentionally contain pseudocode or <input> markers.
SHELL_FILES = FILES[:7] + ("features/stress/STRESS_USER_GUIDE.md",)


def read(name):
    return (ROOT / name).read_text(encoding="utf-8")


def shells(name):
    return re.findall(r"^```(?:bash|sh)\s*\n(.*?)^```\s*$", read(name), re.M | re.S)


class MigrationDocs(unittest.TestCase):
    def test_complete_defaults_keep_admin_and_monitoring_boundaries(self):
        text = read(DELIVERY)
        for option in ("--disable-web", "--npu-allowed-models none",
                       "--deny-npu-reference-generation", "--npu-allowed-devices 0"):
            self.assertIn(option, text)
        self.assertIn("不全选、不自动执行或生成参考", text)
        self.assertIn("Monitoring-only", text)
        self.assertIn("stress.enabled: false", text)
        self.assertIn("--deny-npu-reference-generation", read(MODEL))
        self.assertNotIn("缺省不允许模型任务", read(MODEL))
        config = read("configs/catmonitor.yaml")
        self.assertRegex(config, r"(?m)^stress:\s*\n\s+enabled: false\s*\n\s+web_enabled: false")

    def test_retired_guides_are_not_reintroduced(self):
        for name in RETIRED:
            self.assertFalse((ROOT / "docker" / name).exists(), name)

    def test_local_links_and_fences(self):
        for name in FILES:
            with self.subTest(document=name):
                path = ROOT / name
                text = read(name)
                opened = None
                for line_number, line in enumerate(text.splitlines(), 1):
                    match = re.match(r"^\s{0,3}(`{3,}|~{3,})(.*)$", line)
                    if not match:
                        continue
                    marker, suffix = match.groups()
                    if opened is None:
                        opened = (marker, line_number)
                    elif marker[0] == opened[0][0] and len(marker) >= len(opened[0]) and not suffix.strip():
                        opened = None
                self.assertIsNone(opened, (name, "unclosed fence", opened))
                for target in re.findall(r"\]\(([^)]+)\)", text):
                    target = target.strip().removeprefix("<").removesuffix(">")
                    if re.match(r"^[A-Za-z][A-Za-z0-9+.-]*:", target) or target.startswith(("#", "//")):
                        continue
                    destination = unquote(target.split("#", 1)[0])
                    self.assertTrue((path.parent / destination).exists(), (name, target))

    @unittest.skipUnless(shutil.which("bash"), "Bash is unavailable; shell syntax remains unverified")
    def test_shell_syntax(self):
        checked = 0
        for name in SHELL_FILES:
            for block_number, shell in enumerate(shells(name), 1):
                with self.subTest(document=name, block=block_number):
                    result = subprocess.run(["bash", "-n"], input=shell, text=True, capture_output=True)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    checked += 1
        self.assertGreater(checked, 0)

    def test_a3_patch_chain_has_one_explicit_authority(self):
        candidates = [(name, shell) for name in SHELL_FILES for shell in shells(name)
                      if "--compat-profile portable-model-sdc-memory-job-v1" in shell]
        self.assertEqual(len(candidates), 1, "Keep one executable A3 patch chain, not drifting copies")
        name, shell = candidates[0]
        self.assertEqual(name, DELIVERY)
        self.assertEqual(re.findall(r"--patch scripts/stress/patches/ascend_npu_burn/(\S+)", shell), [
            "portable-runtime-v1.patch", "model-sdc-v1.patch",
            "model-memory-v1.patch", "job-parameters-v1.patch"])
        self.assertIn("--enable-model-sdc", shell)
        for forbidden in ("a2-upstream-base.patch", "a2-cann83.patch", "a2-portable-sizing.patch"):
            self.assertNotIn(forbidden, shell)
        self.assertIn("STRESS_JOB_RELEASE_GUIDE.md", read(DEVELOPER))
        self.assertIn("codex/stress-job-parameters", read(DELIVERY))
        self.assertNotIn("codex/npu-model-a3-migration", "\n".join(shells(DELIVERY)))

    def test_a3_models_keep_hardware_and_memory_boundaries(self):
        text = read(MODEL)
        for required in ("checkpointed", "DDP", "HBM", "reference_generated", "sdc_passed"):
            self.assertIn(required, text)
        self.assertRegex(text, r"A3[^\n]*(?:PENDING|NOT_RUN)")
        self.assertRegex(text, r"(?:不是|不支持)[^\n。]*DDP")
        self.assertIn("--npu-model-memory-mode checkpointed", "\n".join(shells(DELIVERY)))

    def test_full_candidate_manual_contract_and_real_parameter_flags(self):
        text = read(DELIVERY)
        shell = "\n".join(shells(DELIVERY))
        self.assertEqual(len(re.findall(r"docker run -d --name ", shell)), 5)
        for value in ("cpu-workload.env", "npu-workload.env", "--read-only", "--network none",
                      "--require-pass", "--npu-operator-parameters", "--hpcg-runtime-seconds",
                      "--stream-threads", "--hpl-mpi-processes", "-addr=:19322", "set -euo pipefail",
                      '--npu-chip-generation "$NPU_CHIP_GENERATION"'):
            self.assertIn(value, shell)
        for forbidden in ("benchmark_check.sh", "cpu-runner.sock", "catmonitor-install",
                          "catmonitor-stress-web", "29592", "fault26"):
            self.assertNotIn(forbidden, shell)
        self.assertIn("**PENDING**", text)

    def test_a2_first_delivery_keeps_distinct_stack_and_hardware_boundary(self):
        text = read(DELIVERY)
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

    def test_hardware_compose_navigation_targets_existing_heading(self):
        navigation = read("docker/README.md")
        self.assertRegex(navigation, r"(?m)^#{1,6}\s+Compose 与手工启动\s*$")
        for hardware in ("generic", "gpu", "npu"):
            name = f"docker/README-{hardware}.md"
            with self.subTest(document=name):
                self.assertTrue("README.md#compose-与手工启动" in read(name), name)
                self.assertFalse("README.md#61-" in read(name), name)

    def test_hardware_readme_details_are_balanced(self):
        for hardware in ("generic", "gpu", "npu"):
            name = f"docker/README-{hardware}.md"
            with self.subTest(document=name):
                depth = 0
                for match in re.finditer(r"</?details(?:\s[^>]*)?>", read(name), re.I):
                    depth += -1 if match[0].startswith("</") else 1
                    self.assertGreaterEqual(depth, 0, (name, "closing details has no opening tag"))
                self.assertEqual(depth, 0, (name, "unclosed details hides subsequent sections"))

    def test_manual_cpu_deployments_do_not_require_compose_and_can_restart(self):
        for hardware in ("generic", "gpu"):
            name = f"docker/README-{hardware}.md"
            text = read(name)
            with self.subTest(document=name):
                self.assertIsNotNone(re.search(
                    r"(?:手工|docker run)[^。\n]{0,100}(?:不需要|不要求|无需)[^。\n]{0,30}Compose", text),
                    (name, "manual deployment must not require Compose"))
                shell = "\n".join(shells(name)).replace("\\\n", " ")
                for action in ("stop", "start"):
                    commands = re.findall(r"(?m)^\s*docker " + action + r"\s+([^\n]+)", shell)
                    self.assertTrue(commands, (name, "missing manual lifecycle command", action))
                    for container in ("catmonitor", "catmonitor-web", "catmonitor-dfee",
                                      "catmonitor-stress-cpu"):
                        self.assertTrue(
                            any(re.search(r"(?<![\w-])" + re.escape(container) + r"(?![\w-])", command)
                                for command in commands),
                            (name, action, container),
                        )

    def test_hardware_deployment_scenarios_are_mutually_exclusive(self):
        for hardware in ("generic", "gpu", "npu"):
            name = f"docker/README-{hardware}.md"
            with self.subTest(document=name):
                paragraphs = (" ".join(paragraph.splitlines())
                              for paragraph in re.split(r"\n\s*\n", read(name)))
                self.assertTrue(any(re.search(
                    r"(?:场景|路线|路径|方式).{0,180}(?:选(?:择)?(?:其)?一|二选一|互斥|不要同时|不要叠加|不能同时|不可叠加)",
                    paragraph) for paragraph in paragraphs),
                    (name, "explain mutually exclusive deployment scenarios"))

    def test_npu_manual_prerequisites_point_to_real_generator_and_mapping_sections(self):
        text = read("docker/README-npu.md")
        self.assertIsNone(re.search(r"第\s*4\s*节\s*(?:的\s*)?(?:(?:CPU|NPU)-only|Full)?\s*generator", text))
        self.assertIsNone(re.search(r"docker top[^\n]*&&\s*exit\s+1", text))
        full_manual = re.search(r"(?ms)^### 7\.3\b.*?(?=^## |\Z)", text)
        self.assertIsNotNone(full_manual, "Keep the complete manual Full deployment section")
        full_text = " ".join(full_manual[0].splitlines())
        self.assertTrue(re.search(r"7\.1", full_text), "Full manual deployment must run its generator")
        self.assertTrue(re.search(r"6\.2.{0,80}(?:设备|映射|参数)", full_text),
                        "Full manual deployment must read generated device arguments")


if __name__ == "__main__":
    unittest.main()
