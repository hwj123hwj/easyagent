import importlib.util
from pathlib import Path
import plistlib
import subprocess
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("registration", Path(__file__).with_name("unregister-desktop-apps.py"))
registration = importlib.util.module_from_spec(spec)
spec.loader.exec_module(registration)


class RegistrationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        self.root_patch = patch.object(registration, "ROOT", self.root)
        self.root_patch.start()
        self.addCleanup(self.root_patch.stop)
        self.run_patch = patch.object(registration.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, "", ""))
        self.run = self.run_patch.start()
        self.addCleanup(self.run_patch.stop)
        self.app = self.bundle(self.root / "desktop/release.noindex/0.3.0/arm64/mac-arm64/EasyAgent.app")

    def bundle(self, path, identifier="com.easyagent.desktop"):
        (path / "Contents").mkdir(parents=True)
        (path / "Contents/Info.plist").write_bytes(plistlib.dumps({"CFBundleIdentifier": identifier}))
        return path

    def test_unregisters_bundle_and_helpers(self):
        helper = self.bundle(self.app / "Contents/Frameworks/EasyAgent Helper.app", "com.easyagent.desktop.helper")
        registration.unregister(self.app, "")
        self.assertEqual([call.args[0][2] for call in self.run.call_args_list], [str(helper), str(self.app)])

    def test_preserves_running_app_or_helper(self):
        for executable in ["MacOS/EasyAgent", "Frameworks/EasyAgent Helper.app/Contents/MacOS/EasyAgent Helper"]:
            with self.assertRaisesRegex(ValueError, "Quit"):
                registration.unregister(self.app, str(self.app) + "/Contents/" + executable + " --type=gpu-process")
        self.run.assert_not_called()

    def test_refuses_installed_app_and_symlink_escape(self):
        installed = self.bundle(self.root / "Applications/EasyAgent.app")
        link = self.app.parent / "outside.app"
        link.symlink_to(installed)
        for app in [installed, link]:
            with self.assertRaisesRegex(ValueError, "unrelated"):
                registration.unregister(app, "")
        self.run.assert_not_called()

    def test_refuses_unrelated_bundle_in_build_directory(self):
        other = self.bundle(self.app.parent / "Other.app", "com.other.app")
        with self.assertRaisesRegex(ValueError, "identifier"):
            registration.unregister(other, "")
        self.run.assert_not_called()

    def test_handles_isolated_installer_mount(self):
        with tempfile.TemporaryDirectory(prefix="easyagent-installer-check.", suffix=".noindex") as stage:
            app = self.bundle(Path(stage).resolve() / "mount/EasyAgent.app")
            registration.unregister(app, "")
        self.run.assert_called_once()

    def test_development_runtime_and_missing_output(self):
        app = self.bundle(self.root / "desktop/node_modules/electron/dist/Electron.app", "com.github.Electron")
        registration.unregister(app, "")
        registration.unregister(self.root / "desktop/release.noindex/missing/EasyAgent.app", "")
        self.run.assert_called_once()

    def test_reports_failures_but_accepts_spotlight_scan_warning(self):
        self.run.return_value = subprocess.CompletedProcess([], 1, "failed to scan: -10814 from spotlight", "")
        registration.unregister(self.app, "")
        self.run.return_value = subprocess.CompletedProcess([], 1, "", "permission denied")
        with self.assertRaisesRegex(RuntimeError, "permission denied"):
            registration.unregister(self.app, "")


if __name__ == "__main__":
    unittest.main()
