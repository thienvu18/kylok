#!/usr/bin/env python3
"""Validate a release tag and its committed source, without creating anything."""
# SPDX-License-Identifier: LGPL-3.0-only
import argparse
import json
from pathlib import Path
import re

from importlib.util import module_from_spec, spec_from_file_location

ROOT = Path(__file__).resolve().parents[1]
spec = spec_from_file_location("kylok_package", ROOT / "scripts/package-release.py")
package = module_from_spec(spec)
spec.loader.exec_module(package)


def validate(tag, clean=True, distribution=None):
    """Require a matching existing clean tag and verified source identity before release publication."""
    match = re.fullmatch(r"(go|android)/v([^/]+)", tag)
    if not match:
        raise ValueError("Expected go/vX.Y.Z or android/vX.Y.Z")
    sdk, value = match.groups()
    if value != package.version(sdk):
        raise ValueError(f"{tag} does not match {sdk}/VERSION")
    if int(value.split(".", 1)[0]) >= 1 and "-" not in value:
        release = (ROOT / "ROADMAP.md").read_text().split("## v1 release acceptance", 1)[1]
        gates = re.findall(r"^- \[([ x])\] \*\*", release, re.M)
        if len(gates) != 13 or any(gate != "x" for gate in gates):
            raise ValueError("Stable v1 requires all thirteen release gates, including physical evidence")
    commit = None
    if clean:
        if package.git("status", "--porcelain", "--untracked-files=all"):
            raise ValueError("Release publication requires a clean committed checkout")
        commit = package.git("rev-parse", "HEAD").decode().strip()
        tagged = package.git("rev-parse", "--verify", f"refs/tags/{tag}^{{commit}}").decode().strip()
        if tagged != commit:
            raise ValueError("Check out the exact existing release tag before publishing")
    if distribution:
        manifest = json.loads((Path(distribution) / "kylok-release.json").read_text())
        if manifest["dirty"] or not commit or manifest["git_commit"] != commit:
            raise ValueError("Distribution is dirty or belongs to another source commit")
        for name in ("go", "android"):
            if manifest[name + "_version"] != package.version(name):
                raise ValueError("Distribution SDK versions do not match source")
        import hashlib
        tree = hashlib.sha256()
        for path in package.source_paths():
            tree.update(str(path).encode() + b"\0" + hashlib.sha256((ROOT / path).read_bytes()).digest())
        if tree.hexdigest() != manifest["source_tree_sha256"]:
            raise ValueError("Distribution source tree does not match the release checkout")
    return sdk, value


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tag", required=True)
    parser.add_argument("--distribution", type=Path)
    parser.add_argument("--output", type=Path, help="GitHub Actions output file")
    args = parser.parse_args()
    try:
        sdk, value = validate(args.tag, distribution=args.distribution)
        if args.output:
            with args.output.open("a") as stream:
                stream.write(f"sdk={sdk}\nversion={value}\nprerelease={'true' if value.startswith('0.') or '-' in value else 'false'}\n")
        print(f"Validated existing {sdk} tag {args.tag}; no publication performed.")
    except (ValueError, OSError, package.subprocess.CalledProcessError) as error:
        parser.exit(1, f"{error}\n")
