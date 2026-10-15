"""Hermetic launcher/legacy-view tests; no Docker daemon or hardware required."""
import importlib.util
from contextlib import redirect_stdout
import io
import json
import os
from pathlib import Path
import subprocess
import shutil
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[3]
spec = importlib.util.spec_from_file_location("compose_support", ROOT / "docker/compose_support.py")
support = importlib.util.module_from_spec(spec)
spec.loader.exec_module(support)


class ComposeSupportTest(unittest.TestCase):
    def test_bridge_uses_published_port_not_container_listener(self):
        output = io.StringIO()
        with redirect_stdout(output):
            support.urls("web", "node.example", 'msg="web server starting" addr=:19322',
                         "bridge", {"19322/tcp": [{"HostIp": "0.0.0.0", "HostPort": "19337"}]})
        self.assertIn("WEB_LISTEN=:19322", output.getvalue())
        self.assertIn("WEB_PUBLISHED=0.0.0.0:19337", output.getvalue())
        self.assertIn("STRESS_URL=http://node.example:19337/stress/", output.getvalue())

    def test_bridge_without_mapping_or_with_container_loopback_is_rejected(self):
        with self.assertRaisesRegex(ValueError, "no published host port"):
            support.urls("web", "node.example", 'msg="web server starting" addr=:19322', "bridge", {})
        with self.assertRaisesRegex(ValueError, "container loopback"):
            support.urls("web", "node.example", 'msg="web server starting" addr=127.0.0.1:19322',
                         "bridge", {"19322/tcp": [{"HostIp": "0.0.0.0", "HostPort": "19337"}]})

    def test_loopback_published_port_is_not_advertised_as_public(self):
        output = io.StringIO()
        with redirect_stdout(output):
            support.urls("dfee", "node.example", 'msg="dfee server starting (read-only consumer)" addr=:19323',
                         "bridge", {"19323/tcp": [{"HostIp": "127.0.0.1", "HostPort": "19338"}]})
        self.assertIn("http://127.0.0.1:19338/", output.getvalue())
        self.assertIn("DFEE_LOCAL_ONLY=true", output.getvalue())

    def test_resolved_contract_rejects_stale_privilege_or_wrong_binding(self):
        with tempfile.TemporaryDirectory() as directory:
            folder = Path(directory)
            binding = dict(container="site-npu", image="example/npu:test", privileged=True,
                           device_mode="full", host_device_ids=[2, 5], runtime="runc")
            (folder / "stress-profile.json").write_text(json.dumps(dict(npu=binding)))
            service = dict(container_name="site-npu", image="example/npu:test", privileged=True,
                           network_mode="none", read_only=True,
                           environment=dict(CATMONITOR_NPU_DEVICE_MODE="full", CATMONITOR_NPU_DEVICE_COUNT="2"),
                           devices=[dict(source="/dev/davinci2", target="/dev/davinci2"),
                                    dict(source="/dev/davinci5", target="/dev/davinci5")])
            document = dict(services={"catmonitor": {}, "web": {}, "dfee": {}, "catmonitor-stress-npu": service})
            support.validate_resolved("npu", folder, document)
            for field, value, reason in (("cap_drop", ["ALL"], "stale overlays"),
                                         ("security_opt", ["no-new-privileges:true"], "stale overlays"),
                                         ("privileged", False, "privilege differs"),
                                         ("runtime", "ascend", "runtime differs"),
                                         ("container_name", "old-npu", "Controller binding"),
                                         ("devices", [dict(source="/dev/davinci7", target="/dev/davinci2"),
                                                      dict(source="/dev/davinci5", target="/dev/davinci5")], "device source"),
                                         ("devices", [], "device nodes"),
                                         ("read_only", False, "read-only root")):
                with self.subTest(field=field), self.assertRaisesRegex(ValueError, reason):
                    support.validate_resolved("npu", folder, dict(document, services=dict(
                        document["services"], **{"catmonitor-stress-npu": dict(service, **{field: value})})))
            binding["privileged"] = False
            (folder / "stress-profile.json").write_text(json.dumps(dict(npu=binding)))
            service.update(privileged=False, cap_drop=["ALL"], security_opt=["no-new-privileges:true"])
            support.validate_resolved("npu", folder, document)
            for field, value in (("cap_drop", []), ("security_opt", []), ("cap_add", ["SYS_ADMIN"])):
                with self.subTest(restricted_field=field), self.assertRaisesRegex(ValueError, "restricted NPU"):
                    support.validate_resolved("npu", folder, dict(document, services=dict(
                        document["services"], **{"catmonitor-stress-npu": dict(service, **{field: value})})))
            document["services"]["web"]["volumes"] = [dict(target="/var/run/docker.sock")]
            with self.assertRaisesRegex(ValueError, "Docker socket"):
                support.validate_resolved("npu", folder, document)

    def test_actual_incremented_web_port(self):
        logs = 'msg="web server starting" addr=:19322\nmsg="web server starting" addr=:19325\n'
        result = subprocess.run(["python3", str(ROOT / "docker/compose_support.py"), "urls", "web", "example.test"],
                                input=logs, text=True, capture_output=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("STRESS_URL=http://example.test:19325/stress/", result.stdout)

    def test_no_ready_log_is_not_default_port(self):
        with self.assertRaisesRegex(ValueError, "no guessed default"):
            support.urls("web", "", 'msg="port in use, trying next" addr=:19322')

    def test_loopback_is_not_advertised_as_public(self):
        from contextlib import redirect_stdout
        import io
        output = io.StringIO()
        with redirect_stdout(output):
            support.urls("web", "example.test", 'msg="web server starting" addr=127.0.0.1:5001')
        self.assertIn("WEB_LOCAL_ONLY=true", output.getvalue())
        self.assertIn("http://127.0.0.1:5001/", output.getvalue())

    def test_ipv6_and_dfee_use_actual_listener(self):
        from contextlib import redirect_stdout
        import io
        output = io.StringIO()
        with redirect_stdout(output):
            support.urls("dfee", "2001:db8::1", 'msg="dfee server starting (read-only consumer)" addr="[::]:19326"')
        self.assertIn("DFEE_URL=http://[2001:db8::1]:19326/", output.getvalue())

    def test_generated_selection_and_control_identity(self):
        with tempfile.TemporaryDirectory() as folder:
            folder = Path(folder)
            (folder / "catmonitor-stress.yaml").touch()
            (folder / "docker-compose.stress.generated.yml").touch()
            (folder / "stress-profile.json").write_text(json.dumps({
                "control_image": "example/control:test", "cpu": {"enabled": True}, "npu": {"enabled": False}}))
            with patch.dict(os.environ, {"CATMONITOR_IMAGE": "example/control:test"}):
                support.check_generated("cpu", folder)
                with self.assertRaisesRegex(ValueError, "selection"):
                    support.check_generated("full", folder)
            with patch.dict(os.environ, {"CATMONITOR_IMAGE": "wrong/control:test"}):
                with self.assertRaisesRegex(ValueError, "mixed deployment"):
                    support.check_generated("cpu", folder)

    def test_legacy_keeps_canonical_security_and_runtime(self):
        try:
            import yaml
        except ImportError:
            self.skipTest("PyYAML unavailable: legacy schema fixture NOT_RUN")
        with tempfile.TemporaryDirectory() as folder:
            folder = Path(folder)
            state = folder / "state"
            state.mkdir()
            socket = folder / "docker.sock"
            socket.touch()
            generated = folder / "generated.yml"
            generated.write_text("services:\n  catmonitor-stress-npu:\n    runtime: ascend\n"
                                 "    privileged: false\n    devices: ['%s:/dev/davinci5']\n"
                                 "    environment: {NPU_BURN_DEVICE: '0', ASCEND_RT_VISIBLE_DEVICES: '0'}\n" % (folder / "davinci5"))
            files = [ROOT / "docker/docker-compose.yml", ROOT / "docker/docker-compose.npu.yml",
                     ROOT / "docker/docker-compose.stress.yml", generated]
            originals = [file.read_bytes() for file in files]
            with patch.dict(os.environ, {"CATMONITOR_DOCKER_SOCKET": str(socket), "CATMONITOR_STRESS_STATE_DIR": str(state)}):
                support.legacy("npu", folder, files)
            for file, original in zip(files, originals):
                self.assertEqual(file.read_bytes(), original)
            base = json.loads((folder / "00.json").read_text())
            self.assertEqual(base["version"], "2.4")
            self.assertNotIn("docker.sock", json.dumps(base["services"]["web"]))
            stress = json.loads((folder / "02.json").read_text())
            self.assertNotIn("catmonitor-stress-cpu", stress["services"])
            npu = stress["services"]["catmonitor-stress-npu"]
            canonical = yaml.safe_load(files[2].read_text())["services"]["catmonitor-stress-npu"]
            for key in ("read_only", "privileged", "network_mode", "tmpfs", "healthcheck", "environment", "volumes"):
                self.assertEqual(npu[key], canonical[key], key)
            override = json.loads((folder / "03.json").read_text())["services"]["catmonitor-stress-npu"]
            self.assertEqual(override["runtime"], "ascend")
            self.assertFalse(override["privileged"])
            self.assertEqual(override["devices"], [str(folder / "davinci5") + ":/dev/davinci5"])
            self.assertNotIn("create_host_path", json.dumps(stress))

    def test_legacy_missing_guarded_source_fails_closed(self):
        try:
            import yaml  # noqa: F401
        except ImportError:
            self.skipTest("PyYAML unavailable: legacy schema fixture NOT_RUN")
        with tempfile.TemporaryDirectory() as folder:
            missing = str(Path(folder) / "missing-config.yaml")
            with patch.dict(os.environ, {"CATMONITOR_CONFIG": missing}):
                with self.assertRaisesRegex(ValueError, "absent"):
                    support.legacy("monitoring", folder, [ROOT / "docker/docker-compose.config.yml"])
            self.assertFalse(Path(missing).exists())

    def test_legacy_unknown_profile_not_silently_activated(self):
        try:
            import yaml  # noqa: F401
        except ImportError:
            self.skipTest("PyYAML unavailable: legacy schema fixture NOT_RUN")
        with tempfile.TemporaryDirectory() as folder:
            source = Path(folder) / "unknown.yml"
            source.write_text("services:\n  unknown: {image: example/test, profiles: [admin-only]}\n")
            with self.assertRaisesRegex(ValueError, "unknown Compose profile"):
                support.legacy("monitoring", folder, [source])


class ComposeLauncherTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.folder = Path(self.temp.name)
        self.trace = self.folder / "trace"
        docker = self.folder / "docker"
        docker.write_text("""#!/bin/bash
printf '%s\\n' "$*" >> "$TRACE"
case "$1 $2" in
  'compose version') [ "${LEGACY:-0}" = 0 ]; exit ;;
  'compose '*)
    if [[ "$*" == *' ps -q '* ]]; then printf 'fake-%s\\n' "${@: -1}"; fi
    if [[ " $* " == *' config '* ]]; then
      [ "${INVALID:-0}" != 1 ] || exit 1
      if [ -n "${RESOLVED_JSON:-}" ]; then printf '%s\\n' "$RESOLVED_JSON";
      else printf '{"services":{"catmonitor":{},"web":{},"dfee":{}}}\\n'; fi
    fi
    exit 0 ;;
  'inspect --format')
    if [[ "$3" == *Running* ]]; then echo true;
    elif [[ "$3" == *NetworkMode* ]]; then echo "${NETWORK:-host}";
    elif [[ "$3" == *NetworkSettings.Ports* ]]; then echo "${PUBLISHED:-null}";
    else echo 2026-10-14T00:00:00Z; fi ;;
  'container inspect')
    if [ -n "${FOREIGN_OWNER:-}" ]; then echo "$FOREIGN_OWNER"; else exit 1; fi ;;
  'info --format') printf '{"runc":{}}\\n' ;;
  'logs --since')
    if [[ "$4" == fake-web ]]; then echo 'msg="web server starting" addr=:19324';
    else echo 'msg="dfee server starting (read-only consumer)" addr=:19325'; fi ;;
  *) exit 1 ;;
esac
""")
        docker.chmod(0o755)
        compose = self.folder / "docker-compose"
        compose.write_text("#!/bin/bash\nprintf 'legacy %s\\n' \"$*\" >> \"$TRACE\"\nexit 0\n")
        compose.chmod(0o755)
        self.env = dict(os.environ, PATH=str(self.folder) + os.pathsep + os.environ["PATH"], TRACE=str(self.trace))

    def invoke(self, *args, **env):
        return subprocess.run(["bash", str(ROOT / "docker/compose.sh"), *args], text=True, capture_output=True, env=dict(self.env, **env))

    def test_monitoring_v2_file_selection(self):
        result = self.invoke("--hardware", "npu", "config")
        self.assertEqual(result.returncode, 0, result.stderr)
        trace = self.trace.read_text()
        self.assertIn("docker-compose.npu.yml", trace)
        self.assertNotIn("docker-compose.stress.yml", trace)
        self.assertNotIn("--profile", trace)

    def test_config_failure_prevents_up(self):
        result = self.invoke("up", INVALID="1")
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("up -d", self.trace.read_text())

    def test_monitoring_up_preserves_compose_only_entry(self):
        result = self.invoke("up", COMPOSE_PROFILES="stress-npu")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotIn("COMPOSE_CONTRACT=PASS", result.stderr)
        self.assertIn("up -d", self.trace.read_text())

    def test_extra_overlay_is_appended_after_canonical_files(self):
        overlay = self.folder / "sidecar.yml"
        overlay.write_text("services:\n  web: {network_mode: bridge}\n")
        result = self.invoke("--project", "sidecar", "--overlay", str(overlay), "config")
        self.assertEqual(result.returncode, 0, result.stderr)
        trace = self.trace.read_text()
        self.assertLess(trace.index("docker-compose.yml"), trace.index(str(overlay)))
        self.assertIn("-p sidecar", trace)

    def test_explicit_backend_legacy_or_missing_v2(self):
        try:
            import yaml  # noqa: F401
        except ImportError:
            self.skipTest("PyYAML unavailable")
        result = self.invoke("--backend", "legacy", "config")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("COMPOSE_BACKEND=docker-compose", result.stderr)
        result = self.invoke("--backend", "v2", "up", LEGACY="1")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("v2 was requested", result.stderr)

    def npu_fixture(self, runtime="runc"):
        generated = self.folder / "generated"
        generated.mkdir()
        for name in ("catmonitor-stress.yaml", "docker-compose.stress.generated.yml"):
            (generated / name).touch()
        binding = dict(enabled=True, container="sidecar-npu", image="example/npu:test",
                       runtime=runtime, privileged=True, device_mode="full", host_device_ids=[2, 5])
        profile = dict(control_image="example/control:test", cpu=dict(enabled=False), npu=binding)
        (generated / "stress-profile.json").write_text(json.dumps(profile))
        npu = dict(container_name="sidecar-npu", image="example/npu:test", runtime=runtime,
                   privileged=True, network_mode="none", read_only=True,
                   environment=dict(CATMONITOR_NPU_DEVICE_MODE="full", CATMONITOR_NPU_DEVICE_COUNT="2"),
                   devices=[dict(source="/dev/davinci2", target="/dev/davinci2"),
                            dict(source="/dev/davinci5", target="/dev/davinci5")])
        services = {name: dict(image="example/control:test") for name in ("catmonitor", "web", "dfee")}
        services["catmonitor-stress-npu"] = npu
        return generated, json.dumps(dict(services=services))

    def test_foreign_workload_name_prevents_any_up(self):
        generated, document = self.npu_fixture()
        result = self.invoke("--hardware", "npu", "--mode", "npu", "--generated", str(generated), "up",
                             CATMONITOR_IMAGE="example/control:test", RESOLVED_JSON=document, FOREIGN_OWNER="production")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("another project", result.stderr)
        self.assertNotIn("up -d", self.trace.read_text())

    def test_uninstalled_runtime_prevents_any_up_without_switching(self):
        generated, document = self.npu_fixture("ascend")
        result = self.invoke("--hardware", "npu", "--mode", "npu", "--generated", str(generated), "up",
                             CATMONITOR_IMAGE="example/control:test", RESOLVED_JSON=document)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("runtime ascend is not installed", result.stderr)
        self.assertNotIn("up -d", self.trace.read_text())

    def test_valid_npu_contract_reaches_up_without_a_burn_command(self):
        generated, document = self.npu_fixture()
        result = self.invoke("--hardware", "npu", "--mode", "npu", "--generated", str(generated), "up",
                             CATMONITOR_IMAGE="example/control:test", RESOLVED_JSON=document)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("up -d", self.trace.read_text())
        self.assertNotIn("stress run", self.trace.read_text())

    def test_urls_are_from_current_start_not_default(self):
        result = self.invoke("--public-host", "node.example", "urls")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("STRESS_URL=http://node.example:19324/stress/", result.stdout)
        self.assertIn("DFEE_URL=http://node.example:19325/", result.stdout)
        self.assertIn("logs --since 2026-10-14", self.trace.read_text())

    def test_legacy_does_not_pass_modern_profiles(self):
        try:
            import yaml  # noqa: F401
        except ImportError:
            self.skipTest("PyYAML unavailable: legacy launcher fixture NOT_RUN")
        result = self.invoke("config", LEGACY="1")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("COMPOSE_BACKEND=docker-compose", result.stderr)
        trace = self.trace.read_text()
        self.assertIn(".json config", trace)
        self.assertNotIn("--profile", trace)

    def test_wrong_selection_fails_before_compose(self):
        result = self.invoke("--hardware", "generic", "--mode", "npu", "up")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.trace.exists())


class RealComposeConfigTest(unittest.TestCase):
    def test_hardware_mode_matrix_and_legacy_contract_equivalence(self):
        try:
            import yaml  # noqa: F401
        except ImportError:
            self.skipTest("PyYAML unavailable: real legacy-view comparison NOT_RUN")
        if not shutil.which("docker") or subprocess.run(["docker", "compose", "version"], capture_output=True).returncode:
            self.skipTest("Docker Compose v2 unavailable: real parser matrix NOT_RUN")
        with tempfile.TemporaryDirectory() as folder:
            folder = Path(folder)
            socket = folder / "docker.sock"
            socket.touch()
            state = folder / "state"
            state.mkdir()
            host = folder / "host"
            for name in ("dev/davinci2", "dev/davinci5", "dev/davinci_manager", "dev/devmm_svm", "dev/hisi_hdc",
                         "usr/local/Ascend/driver/version.info", "etc/ascend_install.info", "usr/local/bin/npu-smi"):
                path = host / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.touch()
            (host / "usr/local/Ascend/driver/lib64").mkdir(parents=True)
            (host / "usr/local/dcmi").mkdir(parents=True)
            env = dict(os.environ, CATMONITOR_IMAGE="example/control:fixture",
                       CATMONITOR_DOCKER_SOCKET=str(socket), CATMONITOR_STRESS_STATE_DIR=str(state))
            matrix = [("generic", "monitoring"), ("gpu", "monitoring"), ("npu", "monitoring"),
                      ("generic", "cpu"), ("gpu", "cpu"), ("npu", "cpu"), ("npu", "npu"), ("npu", "full")]
            for hardware, mode in matrix:
                with self.subTest(hardware=hardware, mode=mode):
                    files = [ROOT / "docker/docker-compose.yml"]
                    if hardware != "generic":
                        files.append(ROOT / ("docker/docker-compose.%s.yml" % hardware))
                    profiles = []
                    if mode != "monitoring":
                        generated = folder / (hardware + "-" + mode)
                        args = ["bash", str(ROOT / "scripts/stress/generate_stress_deployment.sh"),
                                "--output-dir", str(generated), "--control-image", env["CATMONITOR_IMAGE"], "--enable-web"]
                        if mode in ("cpu", "full"):
                            args += ["--cpu-image", "example/cpu:fixture"]
                            profiles += ["stress-cpu"]
                        if mode in ("npu", "full"):
                            args += ["--npu-image", "example/npu:fixture", "--npu-host-root", str(host),
                                     "--npu-runtime", "runc" if mode == "full" else "ascend",
                                     "--npu-chip-generation", "A2", "--npu-burn-device", "0,1",
                                     "--npu-output-dir", str(folder / (mode + "-output"))]
                            if mode != "full":
                                args += ["--npu-security-profile", "restricted"]
                            profiles += ["stress-npu"]
                        result = subprocess.run(args, text=True, capture_output=True, env=env)
                        self.assertEqual(result.returncode, 0, result.stderr)
                        files += [ROOT / "docker/docker-compose.stress.yml", generated / "docker-compose.stress.generated.yml"]
                    if mode == "full":
                        overlay = folder / "sidecar.yml"
                        overlay.write_text("services:\n" + "".join(
                            "  %s:\n    network_mode: bridge\n    ports: ['127.0.0.1:19327-19360:%d'%s]\n" %
                            (service, port, ", '127.0.0.1:19327-19360:9333'" if service == "dfee" else "")
                            for service, port in (("catmonitor", 19320), ("web", 19322), ("dfee", 19323))))
                        files.append(overlay)
                    modern_args = ["docker", "compose", "--project-directory", str(ROOT), "-p", "fixture"]
                    for file in files:
                        modern_args += ["-f", str(file)]
                    for profile in profiles:
                        modern_args += ["--profile", profile]
                    modern = subprocess.run(modern_args + ["config", "--format", "json"], text=True, capture_output=True, env=env)
                    self.assertEqual(modern.returncode, 0, modern.stderr)
                    modern = json.loads(modern.stdout)["services"]
                    support.validate_resolved(mode, generated if mode != "monitoring" else "", dict(services=modern))
                    legacy_dir = folder / ("legacy-" + hardware + "-" + mode)
                    legacy_dir.mkdir()
                    with patch.dict(os.environ, env):
                        support.legacy(mode, legacy_dir, files)
                    legacy_args = ["docker", "compose", "--project-directory", str(ROOT), "-p", "fixture"]
                    for file in sorted(legacy_dir.glob("*.json")):
                        # Optional official v1 schema gate, supplied separately;
                        # no dependency downloads in the test suite itself.
                        if os.environ.get("CATMONITOR_LEGACY_SCHEMA"):
                            import jsonschema
                            schema = json.loads(Path(os.environ["CATMONITOR_LEGACY_SCHEMA"]).read_text())
                            jsonschema.validate(json.loads(file.read_text()), schema)
                        legacy_args += ["-f", str(file)]
                    legacy = subprocess.run(legacy_args + ["config", "--format", "json"], text=True, capture_output=True, env=env)
                    self.assertEqual(legacy.returncode, 0, legacy.stderr)
                    legacy = json.loads(legacy.stdout)["services"]
                    support.validate_resolved(mode, generated if mode != "monitoring" else "", dict(services=legacy))
                    expected_count = {"monitoring": 3, "cpu": 4, "npu": 4, "full": 5}[mode]
                    self.assertEqual(len(modern), expected_count)
                    self.assertEqual(set(modern), set(legacy))
                    for service in modern:
                        for document in (modern, legacy):
                            document[service].pop("profiles", None)
                            for volume in document[service].get("volumes", []):
                                volume.get("bind", {}).pop("create_host_path", None)
                                if volume.get("bind") == {}:
                                    volume.pop("bind", None)
                        self.assertEqual(modern[service], legacy[service], service)


if __name__ == "__main__":
    unittest.main()
