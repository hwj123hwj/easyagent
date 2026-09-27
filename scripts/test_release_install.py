"""Exercise the real installer's download function using local release fixtures."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

SCRIPT = Path(__file__).with_name('install.sh')
SOURCE = SCRIPT.read_text()
DOWNLOAD = SOURCE[SOURCE.index('download_release() ('):SOURCE.index('\n\nif [ "$NEEDS_DOWNLOAD" = true ]; then', SOURCE.index('download_release() ('))]


class InstallTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(); self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.assets = self.root / 'assets'; self.assets.mkdir()
        self.install = self.root / 'bin'; self.install.mkdir()
        self.commands = self.root / 'commands'; self.commands.mkdir()
        self.env = dict(os.environ, PATH=str(self.commands) + ':' + os.environ['PATH'],
                        EA_TEST_ROOT=str(self.root), EA_VERSION='v1.2.3', INSTALL_DIR=str(self.install))
        (self.install / 'easyagent').write_text('previous core')
        self.wrapper('curl', '''import json,os,pathlib,sys
root=pathlib.Path(os.environ['EA_TEST_ROOT']);args=sys.argv[1:]
url=next(a for a in args if a.startswith('https://'))
if url.endswith('/latest'): print(json.dumps({'tag_name':'v1.2.3'},indent=2));sys.exit(0)
source=root/'assets'/url.rsplit('/',1)[-1]
if not source.exists():sys.exit(22)
pathlib.Path(args[args.index('-o')+1]).write_bytes(source.read_bytes())
''')
        self.fixture()

    def wrapper(self, name, body):
        p = self.commands / name; p.write_text('#!' + sys.executable + '\n' + body); p.chmod(0o755)

    def fixture(self, legacy=False, reported='v1.2.3'):
        for p in self.assets.iterdir(): p.unlink()
        core = 'pi-agent' if legacy else 'easyagent'
        (self.assets / (core + '-linux-amd64')).write_text('#!/bin/sh\necho "' + core + ' ' + reported + '"\n')
        if not legacy:
            (self.assets / 'easyagent-bridge-linux-amd64').write_text('#!/bin/sh\necho "easyagent-bridge v1.2.3"\n')
            for name in ['release.json', 'workflow-runtime.mjs', 'workflow-runtime-licenses.tar.gz']:
                (self.assets / name).write_text('fixture ' + name)
        self.checksums()

    def checksums(self):
        lines = []
        for p in sorted(self.assets.iterdir()):
            if p.name != 'checksums.txt': lines.append(hashlib.sha256(p.read_bytes()).hexdigest() + '  ' + p.name + '\n')
        (self.assets / 'checksums.txt').write_text(''.join(lines))

    def run_install(self):
        shell = 'set -euo pipefail\nREPO=fixture/repo\nOS=linux\nARCH=amd64\ninfo() { :; }\nok() { :; }\nfail() { echo "$*" >&2; exit 1; }\n' + DOWNLOAD + '\ndownload_release\n'
        return subprocess.run(['bash'], input=shell, text=True, capture_output=True, env=self.env)

    def assert_unchanged(self, result):
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertEqual((self.install / 'easyagent').read_text(), 'previous core')
        self.assertFalse(list(self.install.glob('.download.*')))

    def test_success_installs_matching_core_bridge_and_bundle(self):
        self.env.pop('EA_VERSION') # Exercise stable-release discovery, too.
        result = self.run_install(); self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('v1.2.3', (self.install / 'easyagent').read_text())
        self.assertTrue((self.install / 'easyagent-bridge').exists())
        self.assertTrue((self.install / 'workflow-runtime-licenses.tar.gz').exists())

    def test_hash_mismatch_keeps_previous_install(self):
        (self.assets / 'workflow-runtime.mjs').write_text('corrupt bundle')
        self.assert_unchanged(self.run_install())

    def test_missing_checksums_never_falls_back_to_main(self):
        (self.assets / 'checksums.txt').unlink()
        self.assert_unchanged(self.run_install())

    def test_version_mismatch_keeps_previous_install(self):
        self.fixture(reported='v9.9.9')
        self.assert_unchanged(self.run_install())

    def test_missing_modern_manifest_rejected(self):
        (self.assets / 'release.json').unlink(); self.checksums()
        self.assert_unchanged(self.run_install())

    def test_invalid_tag_rejected_before_download(self):
        self.env['EA_VERSION'] = 'v1.2.3;touch /tmp/bad'
        self.assert_unchanged(self.run_install())

    def test_verified_legacy_asset_still_supported(self):
        self.fixture(legacy=True)
        legacy = self.assets / 'pi-agent-linux-amd64'
        legacy.write_text('#!/bin/sh\n# pi-agent v1.2.3\nexit 99\n')
        self.checksums()
        result = self.run_install(); self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('pi-agent v1.2.3', (self.install / 'easyagent').read_text())

    def test_partial_install_error_rolls_back_all_components(self):
        old = {'easyagent': 'previous core', 'easyagent-bridge': 'previous bridge',
               'workflow-runtime.mjs': 'previous bundle', 'release.json': 'previous manifest'}
        for name, content in old.items(): (self.install / name).write_text(content)
        real_mv = shutil.which('mv')
        self.wrapper('mv', '''import os,pathlib,subprocess,sys
root=pathlib.Path(os.environ['EA_TEST_ROOT']);marker=root/'mv-failed'
if sys.argv[-1]==str(root/'bin/workflow-runtime.mjs') and not marker.exists():
 marker.touch();sys.exit(1)
sys.exit(subprocess.run([''' + repr(real_mv) + ''',*sys.argv[1:]]).returncode)
''')
        self.assert_unchanged(self.run_install())
        for name, content in old.items(): self.assertEqual((self.install / name).read_text(), content)
        self.assertFalse((self.install / 'workflow-runtime-licenses.tar.gz').exists())


if __name__ == '__main__': unittest.main()
