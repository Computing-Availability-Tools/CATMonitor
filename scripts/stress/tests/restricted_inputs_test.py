#!/usr/bin/env python3
import contextlib
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.dont_write_bytecode = True
spec=importlib.util.spec_from_file_location('site_check',Path(__file__).parents[1]/'check_site_inputs.py')
check=importlib.util.module_from_spec(spec)
spec.loader.exec_module(check)


class SiteContract(unittest.TestCase):
    def test_archive_and_local_only_contract(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp)
            identities={}
            for relative in ('go.mod','scripts/stress/generate_stress_deployment.sh','docker/README-npu.md'):
                p=root/relative
                p.parent.mkdir(parents=True,exist_ok=True)
                p.write_text('public fixture')
                identities[relative]=hashlib.sha256(p.read_bytes()).hexdigest()
            identity=root/'identity.json'
            identity.write_text(json.dumps({'commit':'public-sha','files':identities}))
            env=dict(SOURCE_DIR=tmp,SOURCE_IDENTITY_FILE=str(identity),DOCKER_BIN='/usr/bin/docker',
                DOCKER_ENDPOINT='unix:///test.sock',EXPECTED_DOCKER_ROOT='/test/data',
                DEPLOYMENT_MODE='monitoring',CONTROL_IMAGE='local:control',CONTROL_IMAGE_ID='sha256:fixture',
                EXPECTED_PLATFORM='linux/arm64',CONFIG_DIR=str(root/'config'),STATE_DIR=str(root/'state'),OUTPUT_DIR=str(root/'output'))
            calls=[]
            def docker(argv,**kwargs):
                calls.append(argv)
                if 'info' in argv: out='/test/data'
                elif 'ps' in argv: out=''
                elif 'inspect' in argv: out=json.dumps([dict(Id='sha256:fixture',Os='linux',Architecture='arm64')])
                else: raise AssertionError(argv)
                return subprocess.CompletedProcess(argv,0,out,'')
            with patch.dict(os.environ,env),patch.object(sys,'argv',['check']),patch.object(check.subprocess,'run',docker),patch.object(check.socket,'socket'),contextlib.redirect_stdout(io.StringIO()),contextlib.redirect_stderr(io.StringIO()):
                self.assertEqual(check.main(),0) # no .git needed
                with patch.dict(os.environ,EXPECTED_DOCKER_ROOT='/wrong'):
                    self.assertEqual(check.main(),1)
                with patch.dict(os.environ,CONTROL_IMAGE_ID='sha256:wrong'):
                    self.assertEqual(check.main(),1)
                with patch.dict(os.environ,STATE_DIR=env['OUTPUT_DIR']):
                    self.assertEqual(check.main(),1)
                with patch.object(check.socket,'socket',side_effect=OSError('occupied')):
                    self.assertEqual(check.main(),1)
                offline=root/'inputs.json'
                offline.write_text(json.dumps({'root':tmp,'files':{'missing.whl':'abc'},'images':{'local:base':'sha256:fixture'}}))
                with patch.object(sys,'argv',['check','--offline-inputs',str(offline)]):
                    self.assertEqual(check.main(),1)
            self.assertFalse(any('pull' in c or 'run' in c or 'build' in c for c in calls))
            self.assertTrue(all(c[1:3]==['-H','unix:///test.sock'] for c in calls))
            self.assertTrue(check.verify_files(root,{'../secret':'bad'}))

    def test_failure_exit_not_masked(self):
        env=os.environ.copy()
        env.pop('SOURCE_DIR',None)
        result=subprocess.run([sys.executable,str(Path(check.__file__))],env=env,capture_output=True)
        self.assertNotEqual(result.returncode,0)


if __name__=='__main__':
    unittest.main()
