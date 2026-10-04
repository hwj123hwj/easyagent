#!/usr/bin/env python3
"""Remove only this checkout's test bundles from macOS application search."""
from pathlib import Path
import plistlib
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parent.parent
LSREGISTER = "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"


def unregister(app, commands):
    app = Path(app).resolve()
    outputs = [ROOT / "desktop/release", ROOT / "desktop/release.noindex"]
    stage = app.parent.parent
    mounted_test = (
        app.name == "EasyAgent.app"
        and app.parent.name == "mount"
        and stage.parent == Path(tempfile.gettempdir()).resolve()
        and stage.name.startswith("easyagent-installer-check.")
        and stage.name.endswith(".noindex")
    )
    development = app == ROOT / "desktop/node_modules/electron/dist/Electron.app"
    if not (any(app.is_relative_to(p) for p in outputs) or mounted_test or development):
        raise ValueError(f"Refusing to unregister an installed or unrelated app: {app}")
    if any(line.startswith(str(app) + "/Contents/") for line in commands.splitlines()):
        raise ValueError(f"Quit the test app before unregistering it: {app}")
    if not app.is_dir():
        return
    identifier = plistlib.loads((app / "Contents/Info.plist").read_bytes()).get("CFBundleIdentifier", "")
    if not (identifier.startswith("com.easyagent.") or (development and identifier == "com.github.Electron")):
        raise ValueError(f"Unrelated bundle identifier: {identifier}")
    # Unregister helpers explicitly; -R can trigger Spotlight scans of packages.
    for bundle in [*app.rglob("*.app"), app]:
        result = subprocess.run([LSREGISTER, "-u", str(bundle)], capture_output=True, text=True)
        # lsregister can remove the entry yet return -10814 from its subsequent
        # Spotlight scan. Other failures remain visible to the caller.
        if result.returncode and "-10814" not in result.stdout + result.stderr:
            raise RuntimeError(result.stdout + result.stderr)


if __name__ == "__main__":
    if sys.platform == "darwin":
        commands = subprocess.check_output(["ps", "-axo", "command="], text=True)
        for bundle in sys.argv[1:]:
            unregister(bundle, commands)
