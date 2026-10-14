#!/usr/bin/env python3
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('local_signing', Path(__file__).with_name('prepare-local-signing.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

class LocalSigningTests(unittest.TestCase):
    def test_setup_failure_never_includes_keychain_secret_in_error(self):
        with patch.object(module.subprocess, 'run') as run:
            run.return_value.returncode = 1
            run.return_value.stderr = 'SECRET'
            with self.assertRaisesRegex(RuntimeError, 'Signing setup step failed') as caught:
                module.command(['security', 'unlock-keychain', '-p', 'SECRET', '/private/keychain'])
            self.assertNotIn('SECRET', str(caught.exception))

    def test_incomplete_identity_is_preserved_instead_of_rotating(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            (root / 'password').write_text('test')
            with self.assertRaisesRegex(RuntimeError, 'Incomplete setup'):
                module.prepare(root)
            self.assertEqual((root / 'password').read_text(), 'test')

    def test_existing_identity_missing_keychain_is_not_regenerated(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            (root / 'identity.json').write_text(json.dumps({'version': 1}))
            with self.assertRaisesRegex(RuntimeError, 'refusing to regenerate'):
                module.prepare(root)
            self.assertEqual(json.loads((root / 'identity.json').read_text()), {'version': 1})

    def test_public_setup_directory_is_rejected(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            root.chmod(0o755)
            with self.assertRaisesRegex(RuntimeError, 'private'):
                module.prepare(root)

if __name__ == '__main__':
    unittest.main()
