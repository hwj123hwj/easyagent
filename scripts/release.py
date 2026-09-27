#!/usr/bin/env python3
"""Release policy and deterministic manifests. No credentials or network needed."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys

TAG_RE = re.compile(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-(alpha|beta|rc)\.([1-9][0-9]*))?\Z")
TARGETS = ("linux-amd64", "linux-arm64", "darwin-amd64", "darwin-arm64")
ASSETS = tuple(f"{name}-{target}" for target in TARGETS for name in ("easyagent", "easyagent-bridge")) + (
    "workflow-runtime.mjs", "workflow-runtime-licenses.tar.gz",
)


def git(*args):
    return subprocess.check_output(["git", *args], text=True, stderr=subprocess.PIPE).strip()


def version_key(tag):
    match = TAG_RE.fullmatch(tag)
    if not match:
        raise ValueError("version must be vMAJOR.MINOR.PATCH or vMAJOR.MINOR.PATCH-(alpha|beta|rc).N (N >= 1)")
    major, minor, patch, phase, count = match.groups()
    return (int(major), int(minor), int(patch), {"alpha": 0, "beta": 1, "rc": 2, None: 3}[phase], int(count or 0))


def check_next(tag, tags):
    key = version_key(tag)
    versions = [other for other in tags if TAG_RE.fullmatch(other) and other != tag]
    if any(version_key(other) >= key for other in versions):
        raise ValueError("release must be newer than existing versions; published tags must never be reused or moved")


def release_notes(tag, content):
    # Exact section; do not accidentally use Unreleased or another version's notes.
    match = re.search(r"^## \[" + re.escape(tag) + r"\](?: - \d{4}-\d{2}-\d{2})?\s*\n(.*?)(?=^## |\Z)", content, re.M | re.S)
    if not match or not match.group(1).strip() or "TODO" in match.group(1):
        raise ValueError(f"CHANGELOG.md needs a non-empty ## [{tag}] section without TODO placeholders")
    return match.group(1).strip() + "\n"


def validate(tag, main_ref="origin/main"):
    version_key(tag)
    if git("status", "--porcelain", "--untracked-files=normal"):
        raise ValueError("release checkout must be clean, including untracked files")
    ref = "refs/tags/" + tag
    if git("cat-file", "-t", ref) != "tag":
        raise ValueError("release tags must be annotated (git tag -a or -s), not lightweight")
    sha = git("rev-parse", ref + "^{commit}")
    if git("rev-parse", "HEAD") != sha:
        raise ValueError("checkout does not match the release tag")
    # A merged feature commit is acceptable; an unmerged branch is not.
    subprocess.run(["git", "merge-base", "--is-ancestor", sha, main_ref], check=True, capture_output=True)
    check_next(tag, git("tag", "--list").splitlines())
    notes = release_notes(tag, git("show", sha + ":CHANGELOG.md"))
    return sha, notes


def make_tag(tag, main_ref):
    version_key(tag)
    if git("symbolic-ref", "--short", "HEAD") != "main":
        raise ValueError("create release tags from main")
    if git("status", "--porcelain", "--untracked-files=normal"):
        raise ValueError("working tree must be clean, including untracked files")
    if git("rev-parse", "HEAD") != git("rev-parse", main_ref):
        raise ValueError("main must match the fetched remote main")
    if tag in git("tag", "--list").splitlines():
        raise ValueError("tag already exists")
    check_next(tag, git("tag", "--list").splitlines())
    notes = release_notes(tag, Path("CHANGELOG.md").read_text())
    subprocess.run(["git", "tag", "-a", tag, "-m", f"EasyAgent {tag}\n\n{notes}"], check=True)
    print(f"Created annotated tag {tag}; publish explicitly with: git push origin refs/tags/{tag}")


def manifest(directory, tag, sha):
    version_key(tag)
    if not re.fullmatch(r"[0-9a-f]{40}", sha):
        raise ValueError("manifest requires a full commit SHA")
    directory = Path(directory)
    allowed = set(ASSETS) | {"release.json", "checksums.txt"}
    extra = {p.name for p in directory.iterdir()} - allowed
    if extra:
        raise ValueError(f"unexpected release files: {sorted(extra)}")
    assets = {}
    for name in ASSETS:
        path = directory / name
        if not path.is_file() or path.is_symlink() or path.stat().st_size == 0:
            raise ValueError(f"missing or empty release asset: {name}")
        assets[name] = hashlib.sha256(path.read_bytes()).hexdigest()
    data = {"version": tag, "commit": sha, "assets": assets}
    (directory / "release.json").write_text(json.dumps(data, sort_keys=True, indent=2) + "\n")
    assets["release.json"] = hashlib.sha256((directory / "release.json").read_bytes()).hexdigest()
    (directory / "checksums.txt").write_text("".join(f"{digest}  {name}\n" for name, digest in sorted(assets.items())))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    for command in ("validate", "tag"):
        sub = commands.add_parser(command)
        sub.add_argument("version")
        sub.add_argument("--main-ref", default="origin/main")
        if command == "validate":
            sub.add_argument("--output")
            sub.add_argument("--notes-file")
    sub = commands.add_parser("manifest")
    sub.add_argument("version")
    sub.add_argument("sha")
    sub.add_argument("directory")
    args = parser.parse_args()
    try:
        if args.command == "tag":
            make_tag(args.version, args.main_ref)
        elif args.command == "manifest":
            manifest(args.directory, args.version, args.sha)
        else:
            sha, notes = validate(args.version, args.main_ref)
            if args.notes_file:
                Path(args.notes_file).write_text(notes)
            if args.output:
                with open(args.output, "a") as output:
                    output.write(f"version={args.version}\nsha={sha}\nprerelease={str('-' in args.version).lower()}\n")
            print(f"Validated {args.version} at {sha}")
    except (ValueError, subprocess.CalledProcessError, OSError) as error:
        print(f"Release rejected: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
