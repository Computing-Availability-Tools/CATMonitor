"""Small Compose compatibility/discovery helpers, not a deployment generator."""
import json
import os
from pathlib import Path
import re
import sys


def fail(message):
    raise ValueError(message)


def check_generated(mode, directory):
    directory = Path(directory)
    for name in ("catmonitor-stress.yaml", "stress-profile.json", "docker-compose.stress.generated.yml"):
        if not (directory / name).is_file():
            fail("incomplete generator output: " + name)
    profile = json.loads((directory / "stress-profile.json").read_text())
    expected = (mode in ("cpu", "full"), mode in ("npu", "full"))
    actual = (profile.get("cpu", {}).get("enabled"), profile.get("npu", {}).get("enabled"))
    if actual != expected:
        fail("--mode does not match the existing generated CPU/NPU selection; regenerate explicitly")
    control_image = profile.get("control_image")
    if control_image and os.environ.get("CATMONITOR_IMAGE") != control_image:
        fail("export CATMONITOR_IMAGE matching the generated control_image; refusing mixed deployment inputs")


def expand_guarded_source(value):
    # Only canonical guarded bind sources need early validation on legacy
    # Compose. Keep all other interpolation/merging in Compose itself.
    def replace(match):
        name, operator, default = match.groups()
        value = os.environ.get(name)
        if operator == ":-":
            return value or default
        if operator == ":?" and not value:
            fail("export " + name + " before using legacy Compose")
        if value is None:
            fail("export " + name + " before using legacy Compose")
        return value
    value = re.sub(r"\$\{([A-Za-z_][A-Za-z0-9_]*)(?:(:-|:\?)([^}]*))?\}", replace, value)
    if "$" in value or not os.path.isabs(value):
        fail("legacy guarded bind source must be an exported absolute host path")
    return value


def legacy(mode, output, filenames):
    try:
        import yaml
    except ImportError:
        fail("legacy Compose needs Python 3 + PyYAML; install python3-yaml/python3-pyyaml or use Compose v2")
    selected = {"stress-cpu": mode in ("cpu", "full"), "stress-npu": mode in ("npu", "full")}
    for index, filename in enumerate(filenames):
        document = yaml.safe_load(Path(filename).read_text())
        if not isinstance(document, dict) or set(document) - {"services", "volumes", "version"}:
            fail("legacy adapter accepts only the canonical services/volumes Compose files")
        services = document.get("services", {})
        for name in list(services):
            if name in ("catmonitor-stress-cpu", "catmonitor-stress-npu") and not selected[name[len("catmonitor-"):]]:
                del services[name]
                continue
            service = services[name]
            profiles = service.pop("profiles", [])
            if any(profile not in selected for profile in profiles):
                fail("unknown Compose profile; refusing to silently drop it")
            for volume in service.get("volumes", []):
                if not isinstance(volume, dict):
                    continue
                bind = volume.get("bind", {})
                if "create_host_path" in bind:
                    if volume.get("type") != "bind" or bind["create_host_path"] is not False:
                        fail("unsupported create_host_path contract")
                    source = expand_guarded_source(volume["source"])
                    if not os.path.exists(source):
                        fail("guarded host bind source is absent; prepare the documented config/state/socket first")
                    # Use exactly the path we checked, not a different .env value.
                    volume["source"] = source
                    del bind["create_host_path"]
                    if not bind:
                        volume.pop("bind", None)
        document["version"] = "2.4"
        # JSON is valid YAML; use a temporary view, never a second maintained
        # set of services. Runtime/devices/security and all values remain intact.
        (Path(output) / ("%02d.json" % index)).write_text(json.dumps(document))


def load_document(filename):
    text = Path(filename).read_text()
    try:
        return json.loads(text)
    except json.JSONDecodeError:
        try:
            import yaml
        except ImportError:
            fail("legacy resolved configuration needs Python 3 + PyYAML")
        return yaml.safe_load(text)


def validate_resolved(mode, directory, document):
    services = document.get("services", {})
    expected = {"catmonitor", "web", "dfee"}
    for kind in ("cpu", "npu"):
        if mode in (kind, "full"):
            expected.add("catmonitor-stress-" + kind)
    # Compose v2 may retain inactive profile services in config output. They
    # must remain profile-gated and are not counted as activated workloads.
    for name, service in services.items():
        if name not in expected and not service.get("profiles"):
            fail("unexpected ungated service in resolved configuration: " + name)
    if not expected.issubset(services):
        fail("resolved configuration is missing a selected service")
    for name in ("web", "dfee"):
        service = services[name]
        if service.get("privileged"):
            fail(name + " must not be privileged")
        for volume in service.get("volumes", []):
            target = volume.get("target") if isinstance(volume, dict) else volume.split(":")[1]
            if target in ("/var/run/docker.sock", "/run/docker.sock"):
                fail(name + " must not mount the Docker socket")
    if mode == "monitoring":
        return
    profile = json.loads((Path(directory) / "stress-profile.json").read_text())
    control = profile.get("control_image")
    if control and any(services[name].get("image") != control for name in ("catmonitor", "web", "dfee")):
        fail("resolved Control/Web/DFeE images differ from the generated control_image")
    for kind in ("cpu", "npu"):
        if mode not in (kind, "full"):
            continue
        binding = profile[kind]
        service = services["catmonitor-stress-" + kind]
        if service.get("container_name") != binding["container"] or service.get("image") != binding["image"]:
            fail("resolved " + kind + " container/image differs from Controller binding; regenerate instead of renaming only Compose")
        if service.get("network_mode") != "none" or service.get("read_only") is not True:
            fail(kind + " workload must retain network none and a read-only root")
        if kind == "npu":
            if binding.get("runtime") and (service.get("runtime") or "runc") != binding["runtime"]:
                fail("NPU runtime differs from generated profile; regenerate --npu-runtime")
            if bool(service.get("privileged", False)) != binding["privileged"]:
                fail("NPU privilege differs from generated profile; regenerate --npu-security-profile")
            if binding["privileged"] and (service.get("cap_drop") or service.get("security_opt")):
                fail("privileged NPU retains restricted cap_drop/security_opt; remove stale overlays and regenerate")
            if not binding["privileged"]:
                drops = {str(value).upper() for value in service.get("cap_drop", [])}
                options = {str(value).lower() for value in service.get("security_opt", [])}
                if "ALL" not in drops or not options.intersection({"no-new-privileges", "no-new-privileges:true"}) or service.get("cap_add"):
                    fail("restricted NPU must retain cap_drop ALL/no-new-privileges without added capabilities")
            environment = service.get("environment", {})
            if not isinstance(environment, dict):
                environment = dict(item.split("=", 1) for item in environment)
            if str(environment.get("CATMONITOR_NPU_DEVICE_MODE")) != binding["device_mode"]:
                fail("resolved NPU device mode differs from generated profile")
            count = len(binding["host_device_ids"])
            if str(environment.get("CATMONITOR_NPU_DEVICE_COUNT")) != str(count):
                fail("resolved NPU mapped device count differs from generated profile")
            node_ids = set()
            for device in service.get("devices", []):
                target = device.get("target") if isinstance(device, dict) else device.split(":")[1]
                match = re.fullmatch(r"/dev/davinci([0-9]+)", target or "")
                if match:
                    source = device.get("source", "") if isinstance(device, dict) else device.split(":")[0]
                    if os.path.basename(source) != "davinci" + match.group(1):
                        fail("resolved NPU device source is not the generated identity mapping")
                    node_ids.add(int(match.group(1)))
            if node_ids != set(binding["host_device_ids"]):
                fail("resolved NPU device nodes differ from generated profile")


def urls(service, public_host, logs, network="host", published=None):
    marker = "web server starting" if service == "web" else "dfee server starting (read-only consumer)"
    addresses = []
    for line in logs.splitlines():
        if marker not in line:
            continue
        match = re.search(r'(?:^|\s)addr=(?:"([^"\s]+)"|([^\s]+))', line)
        if match:
            addresses.append(match.group(1) or match.group(2))
    if not addresses:
        fail(service + " listener is not ready; retry urls or check service logs (no guessed default port)")
    address = addresses[-1]
    match = re.fullmatch(r"(\[[^\]]+\]|[^:]*):([0-9]+)", address)
    if not match or not 0 < int(match.group(2)) <= 65535:
        fail("invalid actual listener address for " + service)
    host, port = match.groups()
    listener = address
    if network != "host":
        mappings = (published or {}).get(port + "/tcp") or []
        if not mappings:
            fail(service + " has no published host port; do not advertise the container listener as a node URL")
        if host in ("localhost", "127.0.0.1", "[::1]"):
            fail(service + " binds container loopback; use a wildcard listener before publishing a bridge port")
        mapping = next((item for item in mappings if ":" not in item.get("HostIp", "")), mappings[0])
        host, port = mapping.get("HostIp", "0.0.0.0"), mapping["HostPort"]
        if ":" in host and not host.startswith("["):
            host = "[" + host + "]"
    wildcard = host in ("", "0.0.0.0", "[::]")
    loopback = host in ("localhost", "127.0.0.1", "[::1]")
    display_host = public_host if wildcard and public_host else ("127.0.0.1" if wildcard else host)
    if ":" in display_host and not display_host.startswith("["):
        display_host = "[" + display_host + "]"
    prefix = "WEB" if service == "web" else "DFEE"
    print(prefix + "_LISTEN=" + listener)
    if network != "host":
        print(prefix + "_PUBLISHED=" + host + ":" + port)
    print(prefix + "_URL=http://" + display_host + ":" + port + "/")
    if service == "web":
        print("STRESS_URL=http://" + display_host + ":" + port + "/stress/")
    if loopback and public_host:
        print(prefix + "_LOCAL_ONLY=true (public-host does not change the bind address)")


def main(args):
    if args[0] == "check-generated":
        check_generated(*args[1:])
    elif args[0] == "legacy":
        legacy(args[1], args[2], args[3:])
    elif args[0] == "urls":
        urls(args[1], args[2], sys.stdin.read(), args[3] if len(args) > 3 else "host",
             json.loads(args[4]) if len(args) > 4 else None)
    elif args[0] == "validate":
        validate_resolved(args[1], args[2], load_document(args[3]))
        print("COMPOSE_CONTRACT=PASS", file=sys.stderr)
    elif args[0] == "workload-names":
        profile = json.loads((Path(args[2]) / "stress-profile.json").read_text())
        for kind in ("cpu", "npu"):
            if args[1] in (kind, "full"):
                print(profile[kind]["container"])
    elif args[0] == "runtime":
        print(load_document(args[1])["services"]["catmonitor-stress-npu"].get("runtime") or "runc")
    elif args[0] == "check-runtime":
        if args[1] not in json.load(sys.stdin):
            fail("Docker runtime " + args[1] + " is not installed; do not silently switch runtimes")
    else:
        fail("unknown Compose helper action")


if __name__ == "__main__":
    try:
        main(sys.argv[1:])
    except (ValueError, KeyError, OSError, TypeError, IndexError) as error:
        print("ERROR: " + str(error), file=sys.stderr)
        sys.exit(1)
