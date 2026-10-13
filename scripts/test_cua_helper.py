"""Exercise native argument validation without reading or controlling a desktop."""
import json, subprocess, sys, unittest
HELPER = sys.argv.pop(1)
NO_TCC = "--no-tcc" in sys.argv
if NO_TCC: sys.argv.remove("--no-tcc")
class NativeContract(unittest.TestCase):
    def invoke(self, actions):
        p = subprocess.run([HELPER, 'validate-actions', '--args', json.dumps({'actions': actions})], capture_output=True, text=True)
        self.assertEqual(json.loads(p.stdout)['success'], p.returncode == 0)
        return p
    def test_supported_actions(self):
        self.assertEqual(self.invoke([{'type':'type','text':'中文😀'}, {'type':'key','key':'cmd+shift+s'}, {'type':'click','x':1919,'y':1079}, {'type':'scroll','dx':-20,'dy':100}, {'type':'wait','ms':1}]).returncode, 0)
    def test_invalid_late_action_rejects_entire_batch(self):
        for last in [{'type':'key','key':'cmd+不存在'}, {'type':'click','x':1920,'y':10}, {'type':'click','x':True,'y':10}, {'type':'click','x':1.5,'y':10}, {'type':'wait','ms':5001}, {'type':'scroll','dy':10001}, {'type':'type','text':''}, {'type':'type','text':'a'*2001}, {'type':'unknown'}]:
            with self.subTest(last=last): self.assertNotEqual(self.invoke([{'type':'key','key':'enter'},last]).returncode,0)
    def test_batch_limits(self):
        for actions in [[], [{'type':'wait','ms':0}]*21, [{'type':'wait','ms':5000}]*11]:
            self.assertNotEqual(self.invoke(actions).returncode, 0)
    def test_target_identity_is_required(self):
        for target in [{}, {'bundle_id':'test.editor','pid':True,'window_id':1}, {'bundle_id':'test.editor','pid':1,'window_id':0}, {'bundle_id':'test.editor','pid':1,'window_id':1.5}, {'bundle_id':'test.editor','pid':1,'window_id':4294967296}]:
            p = subprocess.run([HELPER,'validate-target','--args',json.dumps(target)],capture_output=True,text=True)
            self.assertNotEqual(p.returncode,0)
        p = subprocess.run([HELPER,'validate-target','--args',json.dumps({'bundle_id':'test.editor','pid':1,'window_id':4294967295})],capture_output=True,text=True)
        self.assertEqual(p.returncode,0)
    def test_version_contract(self):
        p = subprocess.run([HELPER,"--version"],capture_output=True,text=True,check=True)
        version=json.loads(p.stdout)
        self.assertEqual(version["protocol_version"],2)
        self.assertNotEqual(version["version"],"dev")

    @unittest.skipIf(NO_TCC, "TCC probes run before packaging, never from a mounted installer")
    def test_permission_status_contract(self):
        p = subprocess.run([HELPER,'status'],capture_output=True,text=True,check=True)
        status=json.loads(p.stdout)
        self.assertIsInstance(status['accessibility'],bool)
        self.assertIsInstance(status['screen_recording'],bool)
        self.assertEqual(status['protocol_version'],2)
if __name__ == '__main__': unittest.main()
