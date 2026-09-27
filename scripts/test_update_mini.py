"""Exercise deployment gates/rollback without touching a real service (Linux/flock)."""
import io, json, os, pathlib, shutil, subprocess, tarfile, tempfile, unittest

SCRIPT = pathlib.Path(__file__).with_name('update-mini.sh')
REVISION = 'a' * 40

class DeploymentTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = pathlib.Path(self.tmp.name)
        fake = self.root / 'commands'; fake.mkdir()
        self.binary = self.root / 'bin/easyagent'; self.binary.parent.mkdir()
        self.binary.write_text('previous'); self.binary.chmod(0o755)
        self.env = dict(os.environ, PATH=str(fake)+':'+os.environ['PATH'], EA_DEPLOY_ROOT=str(self.root/'deploy'), EA_DEPLOY_BIN=str(self.binary), EA_DEPLOY_GO=str(fake/'go'), EA_DEPLOY_SERVICE='fake.service', EA_DEPLOY_API='https://fixture.invalid/repo', EA_DEPLOY_HEALTH='https://fixture.invalid/health', EA_TEST_ROOT=str(self.root), EA_TEST_REVISION=REVISION)
        self.wrapper(fake, 'curl', '''import json,os,pathlib,sys,tarfile,io
args=sys.argv[1:];root=pathlib.Path(os.environ['EA_TEST_ROOT']);rev=os.environ['EA_TEST_REVISION']
if any('/commits/' in a for a in args): print(json.dumps({'sha':rev}))
elif any('/tarball/' in a for a in args):
 with tarfile.open(args[args.index('-o')+1], 'w:gz') as archive:
  info=tarfile.TarInfo('source/third_party/bubbletea/');info.type=tarfile.DIRTYPE;info.mode=0o755;archive.addfile(info)
else: print(json.dumps({'status':'ok','version':rev if os.environ.get('EA_TEST_MODE')!='bad-health' else 'wrong'}))
''')
        self.wrapper(fake, 'go', '''import os,pathlib,sys
root=pathlib.Path(os.environ['EA_TEST_ROOT'])
with (root/'builds').open('a') as out: out.write(' '.join(sys.argv[1:])+'\\n')
if os.environ.get('EA_TEST_MODE')=='bad-tests':sys.exit(1)
if '-o' in sys.argv:
 p=pathlib.Path(sys.argv[sys.argv.index('-o')+1]);p.write_text('new binary');p.chmod(0o755)
''')
        self.wrapper(fake, 'systemctl', '''import os,pathlib,sys
with (pathlib.Path(os.environ['EA_TEST_ROOT'])/'service-log').open('a') as out:out.write(' '.join(sys.argv[1:])+'\\n')
if os.environ.get('EA_TEST_MODE')=='bad-bridge' and 'is-active' in sys.argv and 'bridge.service' in sys.argv:sys.exit(1)
''')
        self.wrapper(fake, 'sleep', '')
    def wrapper(self, directory, name, body):
        p=directory/name;p.write_text('#!/usr/bin/env python3\n'+body);p.chmod(0o755)
    def run_update(self, mode='good'):
        env=dict(self.env,EA_TEST_MODE=mode)
        return subprocess.run(['bash',str(SCRIPT)],env=env,text=True,capture_output=True)
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

if __name__=='__main__': unittest.main()
