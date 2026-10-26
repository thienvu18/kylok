#!/usr/bin/env python3
"""Prepare immutable per-merge SDK source and resume its draft GitHub release."""
# SPDX-License-Identifier: LGPL-3.0-only
import argparse
from importlib.util import module_from_spec, spec_from_file_location
import json
import os
from pathlib import Path
import re
import subprocess
import time
import urllib.error
import urllib.request
import zipfile

ROOT = Path(__file__).resolve().parents[1]
spec = spec_from_file_location("kylok_package", ROOT / "scripts/package-release.py")
package = module_from_spec(spec)
spec.loader.exec_module(package)


def gh(*args, data=None):
    """Call GitHub with structured input and inherited authentication, without logging credentials."""
    result = subprocess.run(["gh", *args], input=json.dumps(data).encode() if data is not None else None,
                            capture_output=True, cwd=ROOT)
    if result.returncode:
        raise RuntimeError("GitHub operation failed: " + result.stderr.decode().strip())
    return result.stdout


def api(path, data=None):
    """Decode a GitHub API response; only an explicit JSON body selects a mutating POST."""
    args = ["api", path]
    if data is not None:
        args += ["--method", "POST", "--input", "-"]
    return json.loads(gh(*args, data=data))


def prerelease(base, build):
    """Append a stable run ID so retries reuse a version and different merges never overwrite it."""
    if not isinstance(build, str) or not re.fullmatch(r"[1-9][0-9]*", build):
        raise ValueError("Build ID must be a positive GitHub run ID")
    if not package.SEMVER.fullmatch(base):
        raise ValueError("Invalid base SDK version")
    return base + ("." if "-" in base else "-dev.") + build


def plan(source, build):
    """Describe version-only source changes and retain the original design draft verbatim."""
    if not isinstance(source, str) or not re.fullmatch(r"[0-9a-f]{40}", source):
        raise ValueError("Source must be a full Git commit SHA")
    bases = {sdk: package.version(sdk) for sdk in ("go", "android")}
    versions = {sdk: prerelease(value, build) for sdk, value in bases.items()}
    files = {f"{sdk}/VERSION": value + "\n" for sdk, value in versions.items()}
    replacements = {bases[sdk]: versions[sdk] for sdk in bases}
    # Release-tag examples must name the packaged versions, rather than the rolling development base.
    # Only installation examples follow the generated version; historical evidence must retain its tested identity.
    guides = [ROOT / name for name in ("README.md", "go/README.md", "docs/sdk.md",
                                       "website/en/install.md", "website/vi/install.md") if (ROOT / name).is_file()]
    for path in guides:
        original = path.read_text()
        pattern = r"(?<![0-9.])(?:" + "|".join(re.escape(base) for base in sorted(replacements, key=len, reverse=True)) + r")(?![0-9A-Za-z.-])"
        rendered = re.sub(pattern, lambda match: replacements[match.group()], original)
        if original != rendered:
            files[path.relative_to(ROOT).as_posix()] = rendered
    files["release/build.json"] = json.dumps({"format": 1, "source_commit": source, "build_id": build,
                                              "base_versions": bases, "versions": versions}, indent=2, sort_keys=True) + "\n"
    return versions, files


def ensure_ref(repo, tag, commit):
    """Create an immutable tag or verify an existing retry target; never move a published name."""
    result = subprocess.run(["gh", "api", f"repos/{repo}/git/ref/tags/{tag}"], capture_output=True)
    if result.returncode == 0:
        if json.loads(result.stdout)["object"]["sha"] != commit:
            raise ValueError(f"Refusing to move existing tag {tag}")
        return
    if b"HTTP 404" not in result.stderr:
        raise RuntimeError("Cannot inspect release tag: " + result.stderr.decode().strip())
    api(f"repos/{repo}/git/refs", {"ref": "refs/tags/" + tag, "sha": commit})


def prepare(repo, source, build, output):
    """Create deterministic tagged release source while keeping protected main free of generated commits."""
    if package.git("status", "--porcelain", "--untracked-files=all"):
        raise ValueError("Release preparation requires a clean checkout")
    if package.git("rev-parse", "HEAD").decode().strip() != source:
        raise ValueError("Check out the exact merged source before release preparation")
    versions, files = plan(source, build)
    parent = api(f"repos/{repo}/git/commits/{source}")
    tree = api(f"repos/{repo}/git/trees", {"base_tree": parent["tree"]["sha"], "tree": [
        {"path": path, "mode": "100644", "type": "blob", "content": value} for path, value in sorted(files.items())]})
    # A source-derived timestamp makes the same run reproducible after an interrupted deployment.
    identity = {"name": "Kylok release automation", "email": "41898282+github-actions[bot]@users.noreply.github.com",
                "date": parent["committer"]["date"]}
    tag = f"release/{build}"
    previous = subprocess.run(["gh", "api", f"repos/{repo}/git/ref/tags/{tag}"], capture_output=True)
    if previous.returncode == 0:
        commit = json.loads(previous.stdout)["object"]["sha"]
        existing = api(f"repos/{repo}/git/commits/{commit}")
        if existing["tree"]["sha"] != tree["sha"] or [p["sha"] for p in existing["parents"]] != [source]:
            raise ValueError("Release source tag belongs to another merge or build")
    elif b"HTTP 404" in previous.stderr:
        commit = api(f"repos/{repo}/git/commits", {"message": f"Release development SDKs for {source}\n",
                     "tree": tree["sha"], "parents": [source], "author": identity, "committer": identity})["sha"]
    else:
        raise RuntimeError("Cannot inspect release source tag")
    ensure_ref(repo, tag, commit)
    subprocess.run(["git", "fetch", "origin", f"refs/tags/{tag}:refs/tags/{tag}"], cwd=ROOT, check=True)
    subprocess.run(["git", "checkout", "--detach", tag], cwd=ROOT, check=True)
    values = {"tag": tag, "commit": commit, **{sdk + "_version": value for sdk, value in versions.items()}}
    if output:
        with output.open("a") as stream:
            stream.write("".join(f"{key}={value}\n" for key, value in values.items()))
    print(json.dumps(values, sort_keys=True))


def restore(repo, tag, output):
    """Create the release draft or recover its prepared artifacts without regenerating signatures on retries."""
    result = subprocess.run(["gh", "release", "view", tag, "--repo", repo, "--json", "isDraft,assets"], capture_output=True)
    if result.returncode:
        # The source tag was checked by prepare; a missing draft is the expected first-run state.
        gh("release", "create", tag, "--repo", repo, "--verify-tag", "--draft", "--prerelease", "--latest=false",
           "--title", f"Kylok development SDKs · {tag}", "--notes", "Publication is being prepared.")
        release = {"assets": []}
    else:
        release = json.loads(result.stdout)
    assets = {item["name"] for item in release["assets"]}
    prepared = "central-bundle.zip" in assets
    if not release.get("isDraft", True) and not prepared:
        raise ValueError("Public release is missing prepared artifacts; it cannot be rebuilt in place")
    if prepared:
        if "kylok-distribution.zip" not in assets:
            raise ValueError("Prepared release is missing its corresponding distribution")
        (ROOT / "out").mkdir(exist_ok=True)
        gh("release", "download", tag, "--repo", repo, "--pattern", "kylok-distribution.zip", "--pattern", "central-bundle.zip",
           "--dir", str(ROOT / "out"), "--clobber")
        destination = ROOT / "out/release"
        destination.mkdir(exist_ok=True)
        with zipfile.ZipFile(ROOT / "out/kylok-distribution.zip") as archive:
            for item in archive.infolist():
                if not (destination / item.filename).resolve().is_relative_to(destination.resolve()):
                    raise ValueError("Unsafe release archive path")
            archive.extractall(destination)
    if output:
        with output.open("a") as stream:
            stream.write(f"build={'false' if prepared else 'true'}\n")
    print("Reusing prepared artifacts" if prepared else "Building new release artifacts")


def tags(repo):
    """Publish SDK version tags pointing to the exact source already built and published to Central."""
    commit = package.git("rev-parse", "HEAD").decode().strip()
    for sdk in ("go", "android"):
        ensure_ref(repo, f"{sdk}/v{package.version(sdk)}", commit)
    subprocess.run(["git", "fetch", "--tags", "origin"], cwd=ROOT, check=True)


def index_go():
    """Request Go's public module index after tagging; this resolves metadata without running tests."""
    version = "v" + package.version("go")
    url = f"https://proxy.golang.org/{package.MODULE}/@v/{version}.info"
    for attempt in range(30):
        try:
            with urllib.request.urlopen(url, timeout=30) as response:
                if json.load(response)["Version"] != version:
                    raise ValueError("Go proxy returned a different version")
            print("Go module is indexed:", version)
            return
        except (urllib.error.URLError, TimeoutError):
            if attempt == 29:
                raise ValueError("Go proxy is not ready; rerun this release without changing its tags") from None
            time.sleep(10)


def main():
    """Expose distinct source/draft/tag/index steps so deployment never invokes the PR test suite."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=["prepare", "restore", "tags", "index-go"])
    parser.add_argument("--repo", default=os.environ.get("GITHUB_REPOSITORY", "thienvu18/kylok"))
    parser.add_argument("--source", default=os.environ.get("GITHUB_SHA"))
    parser.add_argument("--build", default=os.environ.get("GITHUB_RUN_ID"))
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    try:
        if args.command == "prepare":
            prepare(args.repo, args.source, args.build, args.output)
        elif args.command == "restore":
            restore(args.repo, f"release/{args.build}", args.output)
        elif args.command == "tags":
            tags(args.repo)
        else:
            index_go()
    except (ValueError, RuntimeError, OSError, subprocess.CalledProcessError) as error:
        parser.exit(1, str(error) + "\n")


if __name__ == "__main__":
    main()
