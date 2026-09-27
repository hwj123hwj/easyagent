import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

import release

spec = importlib.util.spec_from_file_location("publish_release", Path(__file__).with_name("publish-release.py"))
publisher = importlib.util.module_from_spec(spec)
spec.loader.exec_module(publisher)


class PolicyTest(unittest.TestCase):
    def test_version_grammar_and_order(self):
        tags = ['v0.9.9', 'v0.10.0-alpha.1', 'v0.10.0-beta.1', 'v0.10.0-rc.2', 'v0.10.0-rc.10', 'v0.10.0', 'v1.0.0']
        self.assertEqual(sorted(reversed(tags), key=release.version_key), tags)
        for bad in ['v01.2.3', '1.2.3', 'v1.2', 'v1.2.3-rc.0', 'v1.2.3-rc.01', 'v1.2.3+build', 'v1.2.3;echo bad', 'v1.2.3\n']:
            with self.subTest(bad=bad), self.assertRaises(ValueError): release.version_key(bad)
        release.check_next('v0.12.0', ['v0.11.0', 'v0.12.0-rc.1'])
        with self.assertRaises(ValueError): release.check_next('v0.11.1', ['v0.12.0'])

    def test_notes_must_belong_to_exact_version(self):
        content = '# Changes\n## [Unreleased]\nFuture\n## [v1.2.3] - 2026-09-27\nFixed a bug.\n## [v1.2.2]\nOld\n'
        self.assertEqual(release.release_notes('v1.2.3', content), 'Fixed a bug.\n')
        for bad in ['## [Unreleased]\nFuture', '## [v1.2.3]\n\n## [v1.2.2]\nOld', '## [v1.2.3]\nTODO']:
            with self.assertRaises(ValueError): release.release_notes('v1.2.3', bad)

    def test_real_git_tag_guards(self):
        with tempfile.TemporaryDirectory() as tmp:
            old = os.getcwd(); os.chdir(tmp)
            try:
                def git(*args): return subprocess.run(['git', *args], check=True, capture_output=True, text=True)
                git('init', '-b', 'main'); git('config', 'user.name', 'Release Test'); git('config', 'user.email', 'test@example.invalid')
                Path('CHANGELOG.md').write_text('## [v1.0.0]\nInitial release.\n')
                git('add', '.'); git('commit', '-m', 'initial'); git('update-ref', 'refs/remotes/origin/main', 'HEAD')
                git('tag', 'v1.0.0')
                with self.assertRaises(ValueError): release.validate('v1.0.0')
                git('tag', '-d', 'v1.0.0'); release.make_tag('v1.0.0', 'origin/main')
                self.assertEqual(release.validate('v1.0.0')[0], release.git('rev-parse', 'HEAD'))
                with self.assertRaises(ValueError): release.make_tag('v1.0.0', 'origin/main')
                Path('new.txt').write_text('untracked')
                with self.assertRaises(ValueError): release.validate('v1.0.0')
                git('add', '.'); git('commit', '-m', 'later')
                with self.assertRaises(ValueError): release.validate('v1.0.0')
                git('switch', '-c', 'unmerged'); Path('CHANGELOG.md').write_text('## [v1.1.0]\nA feature.\n')
                git('add', '.'); git('commit', '-m', 'feature'); git('tag', '-a', 'v1.1.0', '-m', 'feature')
                with self.assertRaises(subprocess.CalledProcessError): release.validate('v1.1.0')
            finally: os.chdir(old)


class AssetsTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(); self.addCleanup(self.tmp.cleanup)
        self.directory = Path(self.tmp.name)
        self.tag, self.sha = 'v1.2.3', 'a' * 40
        for name in release.ASSETS: (self.directory / name).write_text('fixture:' + name)

    def test_exact_manifest_and_tamper_detection(self):
        release.manifest(self.directory, self.tag, self.sha)
        hashes = publisher.validate_assets(self.directory, self.tag, self.sha)
        self.assertEqual(len(hashes), len(release.ASSETS) + 2)
        original = (self.directory / 'checksums.txt').read_text()
        release.manifest(self.directory, self.tag, self.sha)
        self.assertEqual(original, (self.directory / 'checksums.txt').read_text())
        (self.directory / release.ASSETS[0]).write_text('corrupted')
        with self.assertRaises(ValueError): publisher.validate_assets(self.directory, self.tag, self.sha)

    def test_missing_and_unexpected_assets_fail_closed(self):
        (self.directory / 'workflow-runtime.mjs').unlink()
        with self.assertRaises(ValueError): release.manifest(self.directory, self.tag, self.sha)
        (self.directory / 'workflow-runtime.mjs').write_text('ok')
        (self.directory / 'old-binary').write_text('leftover')
        with self.assertRaises(ValueError): release.manifest(self.directory, self.tag, self.sha)

    def test_existing_release_is_never_uploaded_or_modified(self):
        release.manifest(self.directory, self.tag, self.sha)
        with mock.patch.object(publisher, 'api', side_effect=[{'sha': self.sha}, {'draft': False}]) as api, \
             mock.patch.object(publisher, 'release_notes', return_value='notes'), \
             mock.patch.object(publisher.subprocess, 'run') as run:
            # The real root changelog exists; notes are supplied independently for this test.
            with self.assertRaisesRegex(ValueError, 'already published'): publisher.publish(self.tag, self.sha, self.directory)
            self.assertEqual(api.call_count, 2); run.assert_not_called()

    def test_upload_failure_leaves_draft_unpublished(self):
        release.manifest(self.directory, self.tag, self.sha)
        with mock.patch.object(publisher, 'api', side_effect=[{'sha': self.sha}, None, {'id': 1}]) as api, \
             mock.patch.object(publisher, 'release_notes', return_value='notes'), \
             mock.patch.object(publisher.subprocess, 'run', side_effect=subprocess.CalledProcessError(1, 'upload')):
            with self.assertRaises(subprocess.CalledProcessError): publisher.publish(self.tag, self.sha, self.directory)
            self.assertFalse(any(call.args[0] == 'PATCH' for call in api.call_args_list))

    def test_only_verified_uploads_are_published(self):
        release.manifest(self.directory, self.tag, self.sha)
        hashes = publisher.validate_assets(self.directory, self.tag, self.sha)
        assets = [{'name': n, 'state': 'uploaded', 'digest': 'sha256:' + h} for n, h in hashes.items()]
        for corrupt in (True, False):
            uploaded = json.loads(json.dumps(assets))
            if corrupt: uploaded[0]['digest'] = 'sha256:' + '0' * 64
            responses = [{'sha': self.sha}, None, {'id': 1}, {'draft': True, 'assets': uploaded}, {'sha': self.sha}, {}]
            with mock.patch.object(publisher, 'api', side_effect=responses) as api, \
                 mock.patch.object(publisher, 'release_notes', return_value='notes'), mock.patch.object(publisher.subprocess, 'run'):
                if corrupt:
                    with self.assertRaisesRegex(ValueError, 'integrity'): publisher.publish(self.tag, self.sha, self.directory)
                    self.assertFalse(any(call.args[0] == 'PATCH' for call in api.call_args_list))
                else:
                    publisher.publish(self.tag, self.sha, self.directory)
                    self.assertEqual(api.call_args.args[0], 'PATCH')
                    self.assertFalse(api.call_args.args[2]['draft'])


if __name__ == '__main__': unittest.main()
