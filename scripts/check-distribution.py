#!/usr/bin/env python3
"""Validate a Kylok distribution before consumption or publication."""
# SPDX-License-Identifier: LGPL-3.0-only
import hashlib
import argparse
import io
import json
from pathlib import Path, PurePosixPath
import tarfile
import xml.etree.ElementTree as ET
import zipfile

ROOT = Path(__file__).resolve().parents[1]


def compare_maven(expected, rebuilt):
    """Refuse publication if a rebuilt binary, sources, POM or module differs from the prepared package."""
    expected, rebuilt = Path(expected), Path(rebuilt)
    files = [p for p in expected.rglob("*") if p.suffix in {".aar", ".jar", ".pom", ".module"}]
    if not files:
        raise ValueError("No prepared Maven artifacts")
    for path in files:
        target = rebuilt / path.relative_to(expected)
        if not target.is_file() or path.read_bytes() != target.read_bytes():
            raise ValueError(f"Rebuilt Maven artifact differs from the verified distribution: {path.relative_to(expected)}")
    print("Rebuilt Maven artifacts match the verified release bytes.")


def check(directory):
    """Check exact distribution contents, digests, package metadata, sources and required notices."""
    root = Path(directory)
    manifest = json.loads((root / "kylok-release.json").read_text())
    assert manifest["format"] == 1
    expected = set(manifest["files"]) | {"kylok-release.json", "SHA256SUMS", ".kylok-distribution"}
    actual = {p.relative_to(root).as_posix() for p in root.rglob("*") if p.is_file()}
    assert actual == expected, (actual ^ expected)
    for name, metadata in manifest["files"].items():
        path = PurePosixPath(name)
        assert not path.is_absolute() and ".." not in path.parts
        assert not (root / name).is_symlink()
        data = (root / name).read_bytes()
        assert len(data) == metadata["size"] and hashlib.sha256(data).hexdigest() == metadata["sha256"], name
    sums = {}
    for line in (root / "SHA256SUMS").read_text().splitlines():
        digest, name = line.split("  ", 1)
        assert name not in sums
        sums[name] = digest
    assert set(sums) == set(manifest["files"]) | {"kylok-release.json"}
    for name, digest in sums.items():
        assert hashlib.sha256((root / name).read_bytes()).hexdigest() == digest, name
    module, version = manifest["module"], "v" + manifest["go_version"]
    prefix = f"{module}@{version}/"
    proxy = root / "go-proxy" / module / "@v"
    with zipfile.ZipFile(proxy / (version + ".zip")) as archive:
        assert len(archive.namelist()) == len(set(archive.namelist()))
        for name in archive.namelist():
            assert name.startswith(prefix) and ".." not in PurePosixPath(name).parts
        for name in ("go.mod", "VERSION", "LICENSE", "COPYING", "NOTICE", "gen/kylok/v1/agent.pb.go", "ports/portstest/policies.go"):
            assert prefix + name in archive.namelist(), name
        assert archive.read(prefix + "go.mod") == (proxy / (version + ".mod")).read_bytes()
        assert archive.read(prefix + "VERSION").decode().strip() == manifest["go_version"]
        for name in ("LICENSE", "COPYING", "NOTICE"):
            assert archive.read(prefix + name) == (root / name).read_bytes(), name
    android = manifest["android_version"]
    repository = root / "maven/io/github/thienvu18/kylok/kylok-dpc" / android
    base = f"kylok-dpc-{android}"
    for suffix in (".aar", "-sources.jar", "-javadoc.jar", ".pom", ".module"):
        assert (repository / (base + suffix)).is_file(), suffix
    pom = ET.fromstring((repository / (base + ".pom")).read_bytes())
    ns = {"m": "http://maven.apache.org/POM/4.0.0"}
    assert pom.findtext("m:groupId", namespaces=ns) == "io.github.thienvu18.kylok"
    assert pom.findtext("m:artifactId", namespaces=ns) == "kylok-dpc"
    assert pom.findtext("m:version", namespaces=ns) == android
    assert pom.findtext("m:packaging", namespaces=ns) == "aar"
    assert "Lesser General Public License" in pom.findtext("m:licenses/m:license/m:name", namespaces=ns)
    dependencies = {(d.findtext("m:groupId", namespaces=ns), d.findtext("m:artifactId", namespaces=ns)) for d in pom.findall("m:dependencies/m:dependency", ns)}
    assert {("androidx.room", "room-runtime"), ("androidx.work", "work-runtime"), ("io.grpc", "grpc-okhttp"), ("com.google.protobuf", "protobuf-javalite")} <= dependencies
    metadata = json.loads((repository / (base + ".module")).read_text())
    assert metadata["component"]["version"] == android
    with zipfile.ZipFile(repository / (base + ".aar")) as archive:
        assert {"AndroidManifest.xml", "classes.jar", "proguard.txt", "res/xml/kylok_device_admin.xml"} <= set(archive.namelist())
        with zipfile.ZipFile(io.BytesIO(archive.read("classes.jar"))) as classes:
            for name in ("LICENSE", "COPYING", "NOTICE"):
                assert classes.read("META-INF/kylok/" + name) == (root / name).read_bytes(), name
            assert "io/github/thienvu18/kylok/dpc/Kylok.class" in classes.namelist()
            assert "io/github/thienvu18/kylok/dpc/AgentStatus.class" in classes.namelist()
    with zipfile.ZipFile(repository / (base + "-sources.jar")) as sources:
        for name in ("LICENSE", "COPYING", "NOTICE"):
            assert sources.read("META-INF/kylok/" + name) == (root / name).read_bytes()
        assert "proto/kylok/v1/agent.proto" in sources.namelist()
    with zipfile.ZipFile(repository / (base + "-javadoc.jar")) as reference:
        assert "index.html" in reference.namelist()
        assert any(name.endswith("/-kylok/index.html") for name in reference.namelist()), "Missing generated Android host reference"
    with zipfile.ZipFile(root / "kylok-maven.zip") as archive:
        for name in archive.namelist():
            assert not PurePosixPath(name).is_absolute() and ".." not in PurePosixPath(name).parts
            assert archive.read(name) == (root / "maven" / name).read_bytes()
    with tarfile.open(root / "kylok-source.tar.gz") as archive:
        names = set(archive.getnames())
        for member in archive.getmembers():
            assert member.isfile() and member.name.startswith("kylok/") and ".." not in PurePosixPath(member.name).parts
            assert not {".git", "build", "out", "lanTest"} & set(PurePosixPath(member.name).parts)
            assert not member.name.endswith((".key", ".p12", ".keystore", ".pem", ".apk", ".aar")), member.name
        for name in ("LICENSE", "COPYING", "NOTICE", "README.vi.md", "CODE_OF_CONDUCT.md", "SECURITY.md", "proto/kylok/v1/agent.proto", "go/go.mod", "android/kylok-dpc/build.gradle.kts", "scripts/package-release.py",
                     "android/kylok-dpc/src/test/resources/network-ca-a.der", "android/kylok-dpc/src/test/resources/network-ca-b.der",
                     "examples/minimal-server/web/index.html", "examples/minimal-server/web/console.js", "website/requirements.txt", "website/en/index.md", "website/vi/index.md"):
            assert "kylok/" + name in names, name
    print(f"Distribution verified: Go {manifest['go_version']}, Android {android}, {len(sums)} checksums; remote publication is separate.")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory", type=Path, nargs="?", default=ROOT / "out/release")
    parser.add_argument("--compare-maven", type=Path, nargs=2, metavar=("PREPARED", "REBUILT"))
    args = parser.parse_args()
    try:
        if args.compare_maven:
            compare_maven(*args.compare_maven)
        else:
            check(args.directory)
    except (AssertionError, OSError, KeyError, ValueError, zipfile.BadZipFile) as error:
        raise SystemExit(f"Invalid Kylok distribution: {error}")
