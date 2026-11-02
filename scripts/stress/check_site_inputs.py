#!/usr/bin/env python3
"""Read-only local-input gate. Never pull, build, create, or restart containers."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import socket
import subprocess
import sys


def verify_files(root, identities):
    failures = []
    for relative, expected in identities.items():
        path = root / relative
        if Path(relative).is_absolute() or '..' in Path(relative).parts:
            failures.append('unsafe relative path')
            continue
        if not path.is_file() or path.is_symlink() or not path.resolve().is_relative_to(root.resolve()):
            failures.append('missing/unsafe input: ' + relative)
            continue
        digest = hashlib.sha256()
        with path.open('rb') as stream:
            for chunk in iter(lambda: stream.read(1024 * 1024), b''):
                digest.update(chunk)
        if digest.hexdigest() != expected:
            failures.append('SHA256 mismatch: ' + relative)
    return failures


def required(name):
    value = os.environ.get(name, '')
    if not value:
        raise ValueError('required site input: ' + name)
    return value


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--offline-inputs', type=Path, help='JSON {root, files, images}; declared input inventory, not proof of dependency closure')
    args = parser.parse_args()
    source = Path(required('SOURCE_DIR'))
    identity = json.loads(Path(required('SOURCE_IDENTITY_FILE')).read_text())
    if not identity.get('commit') or not identity.get('files'):
        raise ValueError('source identity requires commit and files checksums')
    for path in ('go.mod','scripts/stress/generate_stress_deployment.sh','docker/README-npu.md'):
        if path not in identity['files']:
            raise ValueError('source identity missing required file: '+path)
    failures = verify_files(source, identity['files'])
    endpoint = required('DOCKER_ENDPOINT')
    if not endpoint.startswith('unix:///'):
        raise ValueError('only explicit local Unix Docker endpoints are supported')
    docker = [required('DOCKER_BIN'), '-H', endpoint]
    def call(*argv):
        result = subprocess.run(docker+list(argv),capture_output=True,text=True,timeout=30)
        if result.returncode:
            # Do not print auth/proxy/config values or entire daemon stderr.
            raise ValueError('Docker read-only input check failed: '+argv[0])
        return result.stdout.strip()
    actual_root = call('info','--format','{{.DockerRootDir}}')
    if actual_root != required('EXPECTED_DOCKER_ROOT'):
        failures.append('Docker data-root does not match site identity')
    existing = set(call('ps','-a','--format','{{.Names}}').splitlines())
    if existing & {'catmonitor','catmonitor-web','catmonitor-dfee','catmonitor-stress-cpu','catmonitor-stress-npu'}:
        failures.append('canonical container names already in use; do not stop or replace them')
    mode = required('DEPLOYMENT_MODE')
    if mode not in ('monitoring','cpu','npu'):
        failures.append('DEPLOYMENT_MODE must be monitoring, cpu or npu')
    roles = ['CONTROL']+(['CPU_WORKLOAD'] if mode=='cpu' else ['NPU_WORKLOAD'] if mode=='npu' else [])
    images = {required(role+'_IMAGE'):required(role+'_IMAGE_ID') for role in roles}
    if args.offline_inputs:
        inputs=json.loads(args.offline_inputs.read_text())
        if not inputs.get('files') or not inputs.get('images'):
            failures.append('offline inventory must include file and image inputs')
        failures += verify_files(Path(inputs['root']),inputs.get('files',{}))
        images.update(inputs.get('images',{}))
    for image, expected in images.items():
        actual=json.loads(call('image','inspect',image))[0]
        if actual['Id']!=expected or actual['Os']+'/'+actual['Architecture']!=required('EXPECTED_PLATFORM'):
            failures.append('image identity/platform mismatch (check local site inventory)')
    paths=[Path(required(k)) for k in ('CONFIG_DIR','STATE_DIR','OUTPUT_DIR')]
    if len({str(p.resolve()) for p in paths})!=3:
        failures.append('config/state/output must be distinct paths')
    for path in paths:
        if not path.is_absolute() or str(path) in ('/','/etc','/var','/var/lib','/home'):
            failures.append('unsafe deployment path')
        elif path.exists() and any(path.iterdir()):
            failures.append('deployment path not empty; do not overwrite an existing instance')
    # Run this checker in the same network namespace as the intended host-network daemon.
    for port in (19320,19322,19323,9333):
        try:
            with socket.socket() as sock: sock.bind(('0.0.0.0',port))
        except OSError:
            failures.append('required port unavailable: '+str(port))
    if failures:
        for message in failures: print('ERROR: '+message,file=sys.stderr)
        return 1
    print('LOCAL_INPUT_IDENTITY=PASS')
    print('NO_PULL_PERFORMED=true')
    print('OFFLINE_DEPENDENCY_CLOSURE=NOT_VERIFIED')
    print('HARDWARE_ACCEPTANCE=NOT_RUN')
    return 0


if __name__=='__main__':
    try:
        sys.exit(main())
    except (ValueError, OSError, KeyError, TypeError, subprocess.SubprocessError) as error:
        # A malformed local file could embed credentials: only known ValueError
        # messages are returned; never dump JSON contents or process stderr.
        print('ERROR: local input gate failed ('+type(error).__name__+')',file=sys.stderr)
        sys.exit(1)
