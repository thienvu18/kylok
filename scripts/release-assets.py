#!/usr/bin/env python3
"""Prepare transport archives and release notes; never call a remote service."""
# SPDX-License-Identifier: LGPL-3.0-only
import hashlib
import json
from pathlib import Path
import shutil
import sys

from importlib.util import module_from_spec, spec_from_file_location

ROOT = Path(__file__).resolve().parents[1]
spec = spec_from_file_location("kylok_package", ROOT / "scripts/package-release.py")
package = module_from_spec(spec)
spec.loader.exec_module(package)
source = Path(sys.argv[1] if len(sys.argv) > 1 else ROOT / "out/release")
manifest = json.loads((source / "kylok-release.json").read_text())
destination = source.parent / "release-assets"
if destination.exists():
    if not (destination / ".kylok-assets").is_file():
        raise SystemExit("Refusing to replace an unrelated release assets directory")
    shutil.rmtree(destination)
destination.mkdir()
(destination / ".kylok-assets").write_text("Kylok release assets\n")
for name in ("kylok-source.tar.gz", "kylok-maven.zip", "kylok-release.json", "LICENSE", "COPYING", "NOTICE", "sdk.md", "releases.md", "compatibility.md"):
    shutil.copyfile(source / name, destination / name)
entries = [(p.relative_to(source / "go-proxy").as_posix(), p.read_bytes()) for p in (source / "go-proxy").rglob("*") if p.is_file()]
(destination / "kylok-go-proxy.zip").write_bytes(package.zip_bytes(entries))
entries = [(p.relative_to(source).as_posix(), p.read_bytes()) for p in source.rglob("*") if p.is_file()]
(destination / "kylok-distribution.zip").write_bytes(package.zip_bytes(entries))
(destination / "SHA256SUMS").write_text("".join(f"{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.name}\n" for p in sorted(destination.iterdir()) if p.is_file() and not p.name.startswith(".")))
(source.parent / "release-notes.md").write_text(
    f"Kylok development SDKs: Go {manifest['go_version']}, Android {manifest['android_version']}; protocol {manifest['compatibility']['protocol_version']}.\n\n"
    f"Source base commit: `{manifest['git_commit']}`; uncommitted snapshot: `{manifest['dirty']}`. Source tree SHA-256: `{manifest['source_tree_sha256']}`. Pair these development builds from the same revision. API, schema and local formats may change.\n\n"
    "Archives include Go proxy content, Maven dependencies/metadata, corresponding source, LGPL-3.0-only texts, notices and checksums. See sdk.md and releases.md.\n\n"
    "Automated consumer/build results do not certify Android device behavior. Physical evidence and remaining WAN-blocked, install/recovery, self-update and kiosk checks are in compatibility.md.\n"
)
print(f"Prepared release assets and notes under {source.parent}; no publication performed.")
