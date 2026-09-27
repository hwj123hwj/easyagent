"""Exercise deployment gates/rollback without touching a real service (Linux/flock)."""
import io, json, os, pathlib, shutil, subprocess, tarfile, tempfile, unittest
from unittest.mock import patch

SCRIPT = pathlib.Path(__file__).with_name('update-mini.sh')
REVISION = 'a' * 40

def inherited_test_environment():
    return {key: value for key, value in os.environ.items() if not key.startswith('EA_DEPLOY_')}

class DeploymentEnvironmentTest(unittest.TestCase):
    def test_host_deployment_settings_are_not_inherited(self):
        host_settings = {
            'EA_DEPLOY_BRIDGE_BIN': '/host/easyagent-bridge',
            'EA_DEPLOY_REQUIRE_PATH_POLICY': 'true',
            'EA_DEPLOY_REQUIRE_OWNER_ACCESS': 'true',
            'EA_DEPLOY_CONTROL': 'http://host.invalid/admin/deploy',
        }
        with patch.dict(os.environ, host_settings):
            env = inherited_test_environment()
        self.assertFalse(any(key.startswith('EA_DEPLOY_') for key in env))

@unittest.skipUnless(shutil.which('flock'), 'requires flock (Linux)')
class DeploymentTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = pathlib.Path(self.tmp.name)
        fake = self.root / 'commands'; fake.mkdir()
        self.binary = self.root / 'bin/easyagent'; self.binary.parent.mkdir()
        self.binary.write_text('previous'); self.binary.chmod(0o755)
        # systemd starts this test with the real deployment configuration loaded.
        # Keep the fake updater isolated so host paths and capability gates can
        # never leak into a test run.
        self.env = dict(inherited_test_environment(), PATH=str(fake)+':'+os.environ['PATH'], EA_DEPLOY_ROOT=str(self.root/'deploy'), EA_DEPLOY_BIN=str(self.binary), EA_DEPLOY_GO=str(fake/'go'), EA_DEPLOY_SERVICE='fake.service', EA_DEPLOY_API='https://fixture.invalid/repo', EA_DEPLOY_HEALTH='https://fixture.invalid/health', EA_TEST_ROOT=str(self.root), EA_TEST_REVISION=REVISION)
        (self.root/'fixture-guard.py').write_text('import os,sys,pathlib\nroot=pathlib.Path(os.environ["EA_TEST_ROOT"])\nwith (root/"guard-log").open("a") as out:out.write(sys.argv[1]+"\\n")\nif sys.argv[1]=="prepare":\n mode=os.environ.get("EA_TEST_MODE")\n if mode=="busy":sys.exit(75)\n if mode=="guard-failed":sys.exit(1)\n pathlib.Path(sys.argv[4]).write_text("lease")\n')
        self.wrapper(fake, 'curl', '''import json,os,pathlib,sys,tarfile,io
args=sys.argv[1:];root=pathlib.Path(os.environ['EA_TEST_ROOT']);rev=os.environ['EA_TEST_REVISION']
if any('/commits/' in a for a in args): print(json.dumps({'sha':rev}))
elif any('/tarball/' in a for a in args):
 with tarfile.open(args[args.index('-o')+1], 'w:gz') as archive:
  guard=(root/'fixture-guard.py').read_bytes()
  info=tarfile.TarInfo('source/scripts/deploy-guard.py');info.size=len(guard);archive.addfile(info,io.BytesIO(guard))
  info=tarfile.TarInfo('source/third_party/bubbletea/');info.type=tarfile.DIRTYPE;info.mode=0o755;archive.addfile(info)
  if os.environ.get('EA_TEST_RUNTIME')=='1':
   for name in ['package-lock.json','vendor/ZCODE-LICENSE','vendor/SOURCE.md']:
    info=tarfile.TarInfo('source/workflow-runtime/'+name);data=b'{}';info.size=len(data);archive.addfile(info,io.BytesIO(data))
else: print(json.dumps({'status':'ok','version':rev if os.environ.get('EA_TEST_MODE')!='bad-health' else 'wrong'}))
''')
        self.wrapper(fake, 'go', '''import os,pathlib,sys
root=pathlib.Path(os.environ['EA_TEST_ROOT'])
with (root/'builds').open('a') as out: out.write(' '.join(sys.argv[1:])+'\\n')
if os.environ.get('EA_TEST_MODE')=='bad-tests':sys.exit(1)
if '-o' in sys.argv:
 p=pathlib.Path(sys.argv[sys.argv.index('-o')+1])
 if p.name=='easyagent' and os.environ.get('EA_DEPLOY_REQUIRE_PATH_POLICY')=='true':p.write_text('#!/bin/sh\\nif [ "$1" = "--help" ]; then echo "usage: easyagent"; exit 0; fi\\nexit 0\\n')
 else:p.write_text('new binary')
 p.chmod(0o755)
''')
        self.wrapper(fake, 'systemctl', '''import os,pathlib,sys
with (pathlib.Path(os.environ['EA_TEST_ROOT'])/'service-log').open('a') as out:out.write(' '.join(sys.argv[1:])+'\\n')
if os.environ.get('EA_TEST_MODE')=='bad-bridge' and 'is-active' in sys.argv and 'bridge.service' in sys.argv:sys.exit(1)
''')
        self.wrapper(fake, 'npm', "import pathlib; p=pathlib.Path('output');p.mkdir(exist_ok=True);(p/'workflow-runtime.mjs').write_text('new runtime')")
        self.wrapper(fake, 'node', "import os,sys; sys.exit(1 if os.environ.get('EA_TEST_MODE')=='bad-runtime' else 0)")
        self.wrapper(fake, 'sleep', '')
    def wrapper(self, directory, name, body):
        p=directory/name;p.write_text('#!/usr/bin/env python3\n'+body);p.chmod(0o755)
    def run_update(self, mode='good'):
        env=dict(self.env,EA_TEST_MODE=mode)
        return subprocess.run(['bash',str(SCRIPT)],env=env,text=True,capture_output=True)
    def test_busy_does_not_replace_or_restart(self):
        result=self.run_update('busy');self.assertEqual(result.returncode,0,result.stderr)
        self.assertEqual(self.binary.read_text(),'previous')
        self.assertFalse((self.root/'service-log').exists())
        self.assertFalse((self.root/'deploy/current-revision').exists())
        self.assertFalse((self.root/'deploy/failed-revision').exists())
    def test_guard_failure_does_not_replace_or_restart(self):
        self.assertNotEqual(self.run_update('guard-failed').returncode,0)
        self.assertEqual(self.binary.read_text(),'previous')
        self.assertFalse((self.root/'service-log').exists())
    def test_lease_release_on_failure_after_admission(self):
        self.binary.unlink()
        self.assertNotEqual(self.run_update().returncode,0)
        self.assertEqual((self.root/'guard-log').read_text().splitlines(),['prepare','release'])
    def test_success_and_idempotence(self):
        result=self.run_update();self.assertEqual(result.returncode,0,result.stderr)
        self.assertEqual(self.binary.read_text(),'new binary')
        self.assertEqual((self.root/'deploy/current-revision').read_text().strip(),REVISION)
        before=(self.root/'builds').read_text();self.assertEqual(self.run_update().returncode,0)
        self.assertEqual(before,(self.root/'builds').read_text())
    def test_failed_tests_do_not_replace_or_restart(self):
        self.assertNotEqual(self.run_update('bad-tests').returncode,0)
        self.assertEqual(self.binary.read_text(),'previous')
        self.assertFalse((self.root/'service-log').exists())
    def test_wrong_health_version_rolls_back(self):
        self.assertNotEqual(self.run_update('bad-health').returncode,0)
        self.assertEqual(self.binary.read_text(),'previous')
        self.assertEqual((self.root/'service-log').read_text().count('restart fake.service'),2)
        self.assertFalse((self.root/'deploy/current-revision').exists())
        self.assertEqual((self.root/'deploy/failed-revision').read_text().strip(),REVISION)
        before=(self.root/'builds').read_text();self.assertNotEqual(self.run_update().returncode,0)
        self.assertEqual(before,(self.root/'builds').read_text())
    def test_bridge_updated_with_core(self):
        bridge=self.root/'bin/easyagent-bridge';bridge.write_text('previous bridge')
        self.env.update(EA_DEPLOY_BRIDGE_BIN=str(bridge),EA_DEPLOY_BRIDGE_SERVICE='bridge.service')
        result=self.run_update();self.assertEqual(result.returncode,0,result.stderr)
        self.assertEqual(bridge.read_text(),'new binary')
        self.assertIn('restart fake.service bridge.service',(self.root/'service-log').read_text())
    def test_bridge_restored_with_core_on_health_failure(self):
        bridge=self.root/'bin/easyagent-bridge';bridge.write_text('previous bridge')
        self.env.update(EA_DEPLOY_BRIDGE_BIN=str(bridge),EA_DEPLOY_BRIDGE_SERVICE='bridge.service')
        self.assertNotEqual(self.run_update('bad-health').returncode,0)
        self.assertEqual(self.binary.read_text(),'previous')
        self.assertEqual(bridge.read_text(),'previous bridge')
    def test_inactive_bridge_rolls_back_both(self):
        bridge=self.root/'bin/easyagent-bridge';bridge.write_text('previous bridge')
        self.env.update(EA_DEPLOY_BRIDGE_BIN=str(bridge),EA_DEPLOY_BRIDGE_SERVICE='bridge.service')
        self.assertNotEqual(self.run_update('bad-bridge').returncode,0)
        self.assertEqual(self.binary.read_text(),'previous')
        self.assertEqual(bridge.read_text(),'previous bridge')
    def test_missing_required_path_capability_does_not_replace(self):
        self.env['EA_DEPLOY_REQUIRE_PATH_POLICY']='true'
        self.assertNotEqual(self.run_update().returncode,0)
        self.assertEqual(self.binary.read_text(),'previous')
        self.assertFalse((self.root/'service-log').exists())
    def test_missing_owner_capability_does_not_replace_bridge(self):
        bridge=self.root/'bin/easyagent-bridge';bridge.write_text('previous bridge')
        self.env.update(EA_DEPLOY_BRIDGE_BIN=str(bridge),EA_DEPLOY_REQUIRE_OWNER_ACCESS='true')
        self.assertNotEqual(self.run_update().returncode,0)
        self.assertEqual(self.binary.read_text(),'previous')
        self.assertEqual(bridge.read_text(),'previous bridge')
        self.assertFalse((self.root/'service-log').exists())

    def test_runtime_installed_with_core(self):
        self.env['EA_TEST_RUNTIME']='1'
        result=self.run_update();self.assertEqual(result.returncode,0,result.stderr)
        self.assertEqual((self.binary.parent/'workflow-runtime.mjs').read_text(),'new runtime')
    def test_runtime_check_failure_preserves_core(self):
        self.env['EA_TEST_RUNTIME']='1'
        self.assertNotEqual(self.run_update('bad-runtime').returncode,0)
        self.assertEqual(self.binary.read_text(),'previous')
        self.assertFalse((self.root/'service-log').exists())
    def test_runtime_rolls_back_with_core(self):
        self.env['EA_TEST_RUNTIME']='1'
        runtime=self.binary.parent/'workflow-runtime.mjs';runtime.write_text('previous runtime')
        self.assertNotEqual(self.run_update('bad-health').returncode,0)
        self.assertEqual(runtime.read_text(),'previous runtime')
        self.assertEqual(self.binary.read_text(),'previous')
    def test_first_runtime_install_removed_on_rollback(self):
        self.env['EA_TEST_RUNTIME']='1'
        self.assertNotEqual(self.run_update('bad-health').returncode,0)
        self.assertFalse((self.binary.parent/'workflow-runtime.mjs').exists())

import importlib.util, sys, urllib.error
from pathlib import Path
spec=importlib.util.spec_from_file_location('guard', str(SCRIPT.with_name('deploy-guard.py')))
guard=importlib.util.module_from_spec(spec)
spec.loader.exec_module(guard)

class GuardTest(unittest.TestCase):
 def test_busy_unauthorized_old_server_and_success(self):
  with tempfile.TemporaryDirectory() as td:
   env=Path(td)/'env';env.write_text('EA_API_KEY=test-only\n');lease=Path(td)/'lease'
   with patch.object(sys,'argv',['guard','prepare','http://127.0.0.1/admin/deploy',str(env),str(lease)]):
    for status,expected in [(409,75),(401,1),(404,1)]:
     with patch('urllib.request.urlopen',side_effect=urllib.error.HTTPError('http://127.0.0.1',status,'',{},None)):
      self.assertEqual(guard.main(),expected);self.assertFalse(lease.exists())
    with patch('urllib.request.urlopen',return_value=io.BytesIO(json.dumps({'lease':'a'*48,'active':0}).encode())) as req:
     self.assertEqual(guard.main(),0);self.assertEqual(lease.read_text(),'a'*48)
     self.assertEqual(req.call_args.args[0].get_header('Authorization'),'Bearer test-only')

if __name__=='__main__': unittest.main()
