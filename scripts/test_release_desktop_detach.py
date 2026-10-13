import os, pathlib, subprocess, tempfile, unittest
ROOT=pathlib.Path(__file__).resolve().parent.parent
class InstallerDetach(unittest.TestCase):
    def invoke(self, succeed_at):
        source=(ROOT/'scripts/check-desktop-package.sh').read_text()
        function=source[source.index('detach_installer() {'):source.index('\ncleanup() {')]
        with tempfile.TemporaryDirectory() as directory:
            root=pathlib.Path(directory)
            (root/'hdiutil').write_text('#!/bin/bash\nn=0; [[ ! -f "$TASK_COUNT" ]] || n=$(cat "$TASK_COUNT"); n=$((n+1)); echo "$n" > "$TASK_COUNT"\n[[ "$1" == detach && "$2" == "/test mount" && "$#" == 2 ]] || exit 99\n[[ "$n" -ge "$TASK_SUCCESS" ]]\n')
            (root/'sleep').write_text('#!/bin/bash\nexit 0\n')
            for command in ['hdiutil','sleep']: (root/command).chmod(0o755)
            env=dict(os.environ,PATH=str(root)+os.pathsep+os.environ['PATH'],TASK_COUNT=str(root/'count'),TASK_SUCCESS=str(succeed_at))
            result=subprocess.run(['bash','-c',function+'\ndetach_installer "/test mount"'],env=env,text=True,capture_output=True)
            return result,int((root/'count').read_text())
    def test_busy_then_normal_detach(self):
        result,count=self.invoke(3)
        self.assertEqual(result.returncode,0); self.assertEqual(count,3)
    def test_permanent_busy_stops_and_preserves_mount(self):
        result,count=self.invoke(99)
        self.assertNotEqual(result.returncode,0); self.assertEqual(count,5)
        self.assertIn('retained for inspection',result.stderr)
    def test_mounted_helper_avoids_permission_probe(self):
        source=(ROOT/'scripts/check-desktop-package.sh').read_text()
        self.assertIn('test_cua_helper.py "$HELPER" --no-tcc',source)
        self.assertIn('test_cua_helper.py" "$APP/Contents/MacOS/easyagent-cua-helper"',(ROOT/'scripts/build-cua-helper.sh').read_text())
if __name__=='__main__':unittest.main()
