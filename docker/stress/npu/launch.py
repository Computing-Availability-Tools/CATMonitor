"""Use the wheel installed in this image, not CWD/user-site/old PYTHONPATH."""
from pathlib import Path
import json
import os
import re
import sys
import tempfile


def capture_operator_parameters(module):
    """Record exactly the configs returned to this run's engines; no reread."""
    target = os.environ.get('CATMONITOR_NPU_OPERATOR_PARAMETERS_PATH')
    if not target:
        return  # Ordinary upstream CLI use remains unchanged.
    target = Path(target)
    job_id = os.environ.get('CATMONITOR_NPU_JOB_ID', '')
    output = Path(os.environ['NPU_BURN_OUTPUT_DIR'])
    if (not target.is_absolute() or target.parent.resolve() != output.resolve()
            or not re.fullmatch(r'[0-9a-f]{1,64}', job_id) or target.exists()):
        raise ValueError('invalid or reused operator parameter receipt destination')
    original = module.generate_run_cases

    def generate(args):
        cases = original(args)
        if not cases:
            return cases
        proof = dict(schema_version=1, job_id=job_id, chip_generation=args.chip_generation,
                     operators=[dict(name=case.name, config=case.data) for case in cases])
        encoded = json.dumps(proof, sort_keys=True, allow_nan=False).encode('utf-8')
        if len(encoded) > 64 * 1024:
            raise ValueError('operator parameter receipt exceeds 64 KiB')
        fd, name = tempfile.mkstemp(prefix='.operator-parameters-', dir=target.parent)
        try:
            with os.fdopen(fd, 'wb') as stream:
                stream.write(encoded)
                stream.flush()
                os.fsync(stream.fileno())
            os.replace(name, target)
        finally:
            if os.path.exists(name):
                os.unlink(name)
        return cases  # Do not substitute or mutate upstream TestCase objects.

    module.generate_run_cases = generate


def main():
    overlay = Path('/opt/catmonitor/npuburn-python')
    if not (overlay / 'ascend_npu_burn/npu_burn.py').is_file():
        raise SystemExit('ERROR: image-owned NPU Burn overlay is missing')
    sys.path.insert(0, str(overlay))
    from ascend_npu_burn import npu_burn
    capture_operator_parameters(npu_burn)
    npu_burn.main()

if __name__ == '__main__':
    main()
