#!/usr/bin/env python3
"""Build local SDK distributions from current source; never publish or tag."""
# SPDX-License-Identifier: LGPL-3.0-only
import argparse
import datetime
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parents[1]
MODULE = "github.com/thienvu18/kylok/go"
GROUP_PATH = Path("io/github/thienvu18/kylok/kylok-dpc")
SEMVER = re.compile(r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?\Z")


def version(sdk):
    """Validate the independently versioned SDK and the currently supported module major."""
    value = (ROOT / sdk / "VERSION").read_text().strip()
    match = SEMVER.fullmatch(value)
    if not match or any(p.isdigit() and len(p) > 1 and p[0] == "0" for p in (match[4] or "").split(".")):
        raise ValueError(f"Invalid SemVer in {sdk}/VERSION")
    if sdk == "go" and int(match[1]) > 1:
        raise ValueError("Go major 2+ needs a new module path; Android is versioned independently")
    return value


def git(*args):
    """Read checkout identity without taking optional Git locks or mutating its index."""
    return subprocess.check_output(["git", *args], cwd=ROOT, env={**os.environ, "GIT_OPTIONAL_LOCKS": "0"})


def source_paths():
    """Select public rebuildable source while excluding runtime, signing and generated build material."""
    # Respect ignore rules and allow only public source roots. In particular,
    # never pick up runtime data, signing keys, generated trust or build output.
    skip = {".git", ".gradle", ".kotlin", ".tools", "build", "out", "lanTest", "__pycache__"}
    try:
        if not (ROOT / ".git").exists():
            raise subprocess.CalledProcessError(1, "no checkout metadata")
        names = [Path(os.fsdecode(p)) for p in git("ls-files", "--cached", "--others", "--exclude-standard", "-z").split(b"\0") if p]
    except subprocess.CalledProcessError:
        names = []
        for base, dirs, files in os.walk(ROOT):
            dirs[:] = [name for name in dirs if name not in skip]
            names += [(Path(base) / name).relative_to(ROOT) for name in files]
    roots = {".github", "android", "docs", "examples", "go", "proto", "scripts", "tests", "tools", "release", "website"}
    root_files = {".editorconfig", ".gitattributes", ".gitignore", "AGENTS.md", "CONTRIBUTING.md", "COPYING", "LICENSE", "NOTICE", "README.md", "ROADMAP.md", "Makefile", "design.md", "buf.yaml", "buf.gen.yaml", "go.work"}
    suffixes = {".go", ".md", ".kt", ".kts", ".java", ".xml", ".proto", ".mod", ".sum", ".py", ".sh", ".json", ".yml", ".yaml", ".pro", ".bat", ".lockfile", ".html", ".css", ".js"}
    allowed_names = {"LICENSE", "COPYING", "NOTICE", "VERSION", "gradlew", "gradle.properties", "gradle-wrapper.properties", "gradle-wrapper.jar", "apk-metadata", "requirements.txt", "signing-public-key.asc"}
    result = []
    for p in sorted(set(names)):
        if p.is_absolute() or ".." in p.parts or any(part in skip for part in p.parts):
            continue
        if len(p.parts) == 1:
            allowed = p.name in root_files
        else:
            allowed = p.parts[0] in roots and (p.suffix in suffixes or p.name in allowed_names)
        if not allowed:
            continue
        actual = ROOT / p
        if actual.is_symlink():
            raise ValueError(f"Source archive refuses symlink: {p}")
        if actual.is_file():
            result.append(p)
    return result


def zip_bytes(entries):
    """Build a byte-reproducible archive with normalized ordering, timestamps and modes."""
    output = io.BytesIO()
    with zipfile.ZipFile(output, "w", compression=zipfile.ZIP_DEFLATED) as archive:
        for name, data in sorted(entries):
            entry = zipfile.ZipInfo(name, (1980, 1, 1, 0, 0, 0))
            entry.compress_type = zipfile.ZIP_DEFLATED
            entry.external_attr = 0o100644 << 16
            archive.writestr(entry, data)
    return output.getvalue()


def package(output, require_clean=False):
    """Atomically assemble module/Maven/source artifacts, notices, source identity and checksums."""
    go_version, android_version = version("go"), version("android")
    try:
        if not (ROOT / ".git").exists():
            raise subprocess.CalledProcessError(1, "no checkout metadata")
        commit = git("rev-parse", "HEAD").decode().strip()
        dirty = bool(git("status", "--porcelain", "--untracked-files=all"))
        epoch = int(git("show", "-s", "--format=%ct", "HEAD"))
    except subprocess.CalledProcessError:
        commit, dirty, epoch = None, True, 0
    if require_clean and (dirty or not commit):
        raise ValueError("Publishing requires committed source and a clean checkout")
    output = output.absolute()
    if output.is_symlink() or output == ROOT or output in ROOT.parents:
        raise ValueError("Unsafe distribution output directory")
    marker = ".kylok-distribution"
    if output.exists() and not (output / marker).is_file():
        raise ValueError("Existing output is not a task-owned Kylok distribution")
    output.parent.mkdir(parents=True, exist_ok=True)
    staging = Path(tempfile.mkdtemp(prefix=".kylok-distribution-", dir=output.parent))
    try:
        (staging / marker).write_text("Kylok local distribution\n")
        paths = source_paths()
        tree = hashlib.sha256()
        for p in paths:
            tree.update(str(p).encode() + b"\0" + hashlib.sha256((ROOT / p).read_bytes()).digest())
        source = io.BytesIO()
        with gzip.GzipFile(fileobj=source, mode="wb", mtime=0, filename="") as compressed:
            with tarfile.open(fileobj=compressed, mode="w") as archive:
                for p in paths:
                    data = (ROOT / p).read_bytes()
                    info = tarfile.TarInfo("kylok/" + p.as_posix())
                    info.size, info.mtime = len(data), 0
                    info.mode = 0o755 if (ROOT / p).stat().st_mode & 0o111 else 0o644
                    archive.addfile(info, io.BytesIO(data))
        (staging / "kylok-source.tar.gz").write_bytes(source.getvalue())
        proxy = staging / "go-proxy" / MODULE / "@v"
        proxy.mkdir(parents=True)
        v = "v" + go_version
        entries = [(f"{MODULE}@{v}/{p.relative_to('go').as_posix()}", (ROOT / p).read_bytes()) for p in paths if p.parts[0] == "go"]
        (proxy / f"{v}.zip").write_bytes(zip_bytes(entries))
        (proxy / f"{v}.mod").write_bytes((ROOT / "go/go.mod").read_bytes())
        instant = datetime.datetime.fromtimestamp(epoch, datetime.timezone.utc).isoformat().replace("+00:00", "Z")
        (proxy / f"{v}.info").write_text(json.dumps({"Version": v, "Time": instant}) + "\n")
        (proxy / "list").write_text(v + "\n")
        published = ROOT / "out/maven" / GROUP_PATH / android_version
        if not published.is_dir():
            raise ValueError("Publish the Android release to the local Distribution repository first")
        destination = staging / "maven" / GROUP_PATH / android_version
        shutil.copytree(published, destination)
        (staging / "kylok-maven.zip").write_bytes(zip_bytes([(p.relative_to(staging / "maven").as_posix(), p.read_bytes()) for p in destination.rglob("*") if p.is_file()]))
        for name in ("LICENSE", "COPYING", "NOTICE"):
            shutil.copyfile(ROOT / name, staging / name)
        for name in ("compatibility.md", "sdk.md", "releases.md"):
            shutil.copyfile(ROOT / "docs" / name, staging / name)
        dependencies = staging / "dependencies"
        dependencies.mkdir()
        for source, target in (("go/go.mod", "go.mod"), ("go/go.sum", "go.sum"), ("android/kylok-dpc/gradle.lockfile", "android.lockfile")):
            shutil.copyfile(ROOT / source, dependencies / target)
        manifest = {"format": 1, "go_version": go_version, "android_version": android_version, "module": MODULE,
                    "maven": f"io.github.thienvu18.kylok:kylok-dpc:{android_version}", "git_commit": commit,
                    "dirty": dirty, "source_tree_sha256": tree.hexdigest(), "compatibility": json.loads((ROOT / "release/compatibility.json").read_text()), "files": {}}
        for p in sorted(staging.rglob("*")):
            if p.is_file() and p.name != marker:
                data = p.read_bytes()
                manifest["files"][p.relative_to(staging).as_posix()] = {"sha256": hashlib.sha256(data).hexdigest(), "size": len(data)}
        (staging / "kylok-release.json").write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n")
        sums = [(name, item["sha256"]) for name, item in manifest["files"].items()]
        sums.append(("kylok-release.json", hashlib.sha256((staging / "kylok-release.json").read_bytes()).hexdigest()))
        (staging / "SHA256SUMS").write_text("".join(f"{digest}  {name}\n" for name, digest in sorted(sums)))
        if output.exists():
            shutil.rmtree(output)  # Marker was validated above; no unrelated directory is removed.
        staging.rename(output)
    finally:
        if staging.exists():
            shutil.rmtree(staging)
    print(f"Local distribution: {output} (Go {go_version}, Android {android_version}; unpublished)")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", type=Path, default=ROOT / "out/release")
    parser.add_argument("--require-clean", action="store_true")
    args = parser.parse_args()
    try:
        package(args.out, args.require_clean)
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        parser.exit(1, f"{error}\n")
