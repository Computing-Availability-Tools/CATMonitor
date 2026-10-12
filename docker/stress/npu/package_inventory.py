#!/usr/bin/env python3
"""Build gate for all public Python modules/configs, without importing NPU code."""
import argparse
import hashlib
import json
from pathlib import Path


def inventory(root):
    return {p.relative_to(root).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest()
            for p in sorted(root.rglob('*')) if p.is_file() and p.suffix in ('.py', '.json')
            and '__pycache__' not in p.parts}


def validate(root, expected):
    actual = inventory(root)
    if not expected:
        raise ValueError('empty NPU package inventory')
    changed = [name for name, digest in expected.items() if actual.get(name) != digest]
    if changed:
        raise ValueError('missing or changed NPU package files: ' + ', '.join(changed))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=('capture', 'validate'))
    parser.add_argument('--root', required=True, type=Path)
    parser.add_argument('--manifest', required=True, type=Path)
    args = parser.parse_args()
    if args.mode == 'capture':
        content = inventory(args.root)
        if not content:
            raise ValueError('empty NPU source package')
        args.manifest.write_text(json.dumps(content, sort_keys=True) + '\n')
    else:
        content = json.loads(args.manifest.read_text())
        validate(args.root, content)
        print('CATMONITOR_NPU_PACKAGE_COMPLETENESS=PASS files=' + str(len(content)))


if __name__ == '__main__':
    main()
