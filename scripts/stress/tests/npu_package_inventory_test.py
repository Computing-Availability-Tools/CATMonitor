#!/usr/bin/env python3
import importlib.util
from pathlib import Path
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[3]
spec = importlib.util.spec_from_file_location('inventory', ROOT/'docker/stress/npu/package_inventory.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class InventoryTest(unittest.TestCase):
    def test_config_and_module_omission_or_change_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            for name in ('config/base_config.json', 'config/a3_config.json', 'benchmarks/model/gpt3.py', 'benchmarks/op/conv2d.py'):
                path = root/name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text('{}\n')
            expected = module.inventory(root)
            module.validate(root, expected)
            for name in expected:
                path = root/name
                path.unlink()
                with self.assertRaises(ValueError): module.validate(root, expected)
                path.write_text('changed')
                with self.assertRaises(ValueError): module.validate(root, expected)
                path.write_text('{}\n')
            module.validate(root, expected)

    def test_docker_build_contract(self):
        dockerfile = (ROOT/'docker/stress/npu/Dockerfile').read_text()
        self.assertIn('package_inventory.py capture', dockerfile)
        self.assertIn('package_inventory.py validate', dockerfile)
        self.assertIn('COPY --from=npuburn_runtime_package /opt/catmonitor/npuburn-python/', dockerfile)
        self.assertIn('package_inventory.py" "$CONTEXT/package_inventory.py"', (ROOT/'scripts/stress/build_npu_burn_image.sh').read_text())


if __name__ == '__main__':
    unittest.main()
