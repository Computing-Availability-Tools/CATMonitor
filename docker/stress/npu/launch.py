"""Use the wheel installed in this image, not CWD/user-site/old PYTHONPATH."""
from pathlib import Path
import sys

overlay = Path('/opt/catmonitor/npuburn-python')
if not (overlay / 'ascend_npu_burn/npu_burn.py').is_file():
    raise SystemExit('ERROR: image-owned NPU Burn overlay is missing')
sys.path.insert(0, str(overlay))
from ascend_npu_burn.npu_burn import main

if __name__ == '__main__':
    main()
