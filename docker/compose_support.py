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


def urls(service, public_host, logs):
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
    wildcard = host in ("", "0.0.0.0", "[::]")
    loopback = host in ("localhost", "127.0.0.1", "[::1]")
    display_host = public_host if wildcard and public_host else ("127.0.0.1" if wildcard else host)
    if ":" in display_host and not display_host.startswith("["):
        display_host = "[" + display_host + "]"
    prefix = "WEB" if service == "web" else "DFEE"
    print(prefix + "_LISTEN=" + address)
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
        urls(args[1], args[2], sys.stdin.read())
    else:
        fail("unknown Compose helper action")


if __name__ == "__main__":
    try:
        main(sys.argv[1:])
    except (ValueError, KeyError, OSError, TypeError, IndexError) as error:
        print("ERROR: " + str(error), file=sys.stderr)
        sys.exit(1)
