#!/usr/bin/env python3
"""Upload a fully verified draft, then publish once; never edit a published release."""
import hashlib
import json
from pathlib import Path
import subprocess
import sys
from urllib.parse import quote

from release import ASSETS, release_notes, version_key


def api(method, path, data=None, optional=False):
    command = ["gh", "api", "repos/{owner}/{repo}/" + path, "--method", method]
    if data is not None:
        command += ["--input", "-"]
    result = subprocess.run(command, input=json.dumps(data) if data is not None else None,
                            text=True, capture_output=True, check=False)
    if result.returncode:
        if optional and "(HTTP 404)" in result.stderr:
            return None
        raise RuntimeError(result.stderr.strip())
    return json.loads(result.stdout) if result.stdout.strip() else None


def validate_assets(directory, tag, sha):
    directory = Path(directory)
    expected = set(ASSETS) | {"release.json", "checksums.txt"}
    if {p.name for p in directory.iterdir()} != expected:
        raise ValueError("release directory does not contain exactly the required assets")
    hashes = {}
    for name in expected:
        path = directory / name
        if path.is_symlink() or not path.is_file() or not path.stat().st_size:
            raise ValueError(f"invalid release asset: {name}")
        hashes[name] = hashlib.sha256(path.read_bytes()).hexdigest()
    manifest = json.loads((directory / "release.json").read_text())
    if manifest != {"version": tag, "commit": sha, "assets": {n: hashes[n] for n in ASSETS}}:
        raise ValueError("release manifest does not match the requested version, commit or files")
    checksums = "".join(f"{hashes[n]}  {n}\n" for n in sorted(expected - {"checksums.txt"}))
    if (directory / "checksums.txt").read_text() != checksums:
        raise ValueError("checksum manifest is incomplete or incorrect")
    return hashes


def publish(tag, sha, directory):
    version_key(tag)
    hashes = validate_assets(directory, tag, sha)
    encoded = quote(tag, safe="")
    if api("GET", "commits/" + encoded)["sha"] != sha:
        raise ValueError("remote tag no longer matches the built commit")
    notes = release_notes(tag, Path("CHANGELOG.md").read_text())
    current = api("GET", "releases/tags/" + encoded, optional=True)
    if current and not current["draft"]:
        raise ValueError("release already published; use a new version, never replace its assets")
    if current and current["target_commitish"] != sha:
        raise ValueError("existing draft belongs to a different source revision")
    if current is None:
        current = api("POST", "releases", {"tag_name": tag, "target_commitish": sha,
                      "name": "EasyAgent " + tag, "body": notes, "draft": True,
                      "prerelease": "-" in tag})
    # --clobber is only used for a draft interrupted during an earlier upload.
    subprocess.run(["gh", "release", "upload", tag, "--clobber",
                    *[str(Path(directory) / name) for name in sorted(hashes)]], check=True)
    uploaded = api("GET", f"releases/{current['id']}")
    if not uploaded["draft"]:
        raise ValueError("release was published externally during upload")
    actual = {asset["name"]: asset for asset in uploaded["assets"]}
    if set(actual) != set(hashes):
        raise ValueError("uploaded release has missing or unexpected assets; draft retained")
    for name, digest in hashes.items():
        if actual[name].get("digest") != "sha256:" + digest or actual[name].get("state") != "uploaded":
            raise ValueError(f"remote asset integrity check failed: {name}; draft retained")
    if api("GET", "commits/" + encoded)["sha"] != sha:
        raise ValueError("remote tag changed before publication")
    api("PATCH", f"releases/{current['id']}", {"draft": False, "body": notes,
        "prerelease": "-" in tag, "make_latest": "false" if "-" in tag else "true"})
    print(f"Published {tag} at {sha}, {len(hashes)} verified assets")


if __name__ == "__main__":
    try:
        if len(sys.argv) != 4:
            raise ValueError("usage: publish-release.py VERSION SHA DIRECTORY")
        publish(*sys.argv[1:])
    except (ValueError, OSError, RuntimeError, subprocess.CalledProcessError) as error:
        print(f"Release not published: {error}", file=sys.stderr)
        sys.exit(1)
