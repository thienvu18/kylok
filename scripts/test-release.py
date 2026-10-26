#!/usr/bin/env python3
"""Exercise release failure gates and a tampered real distribution."""
# SPDX-License-Identifier: LGPL-3.0-only
from importlib.util import module_from_spec, spec_from_file_location
import json
import hashlib
import os
from pathlib import Path
import shutil
import tempfile
import unittest
from unittest.mock import patch
import subprocess
import zipfile

ROOT = Path(__file__).resolve().parents[1]


def load(name, filename):
    spec = spec_from_file_location(name, ROOT / "scripts" / filename)
    module = module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


release = load("kylok_release_validation", "validate-release.py")
distribution = load("kylok_distribution_validation", "check-distribution.py")
source_release = load("kylok_source_release", "release-source.py")
central = load("kylok_central", "central.py")


class AutomaticPublication(unittest.TestCase):
    def test_central_bundle_rejects_tampered_payloads_and_unexpected_paths(self):
        with tempfile.TemporaryDirectory(prefix="kylok-central-bundle-") as directory:
            output = Path(directory) / "bundle.zip"
            prefix = (central.package.GROUP_PATH / central.package.version("android")).as_posix() + "/kylok-dpc-" + central.package.version("android")
            entries = []
            for suffix in (".aar", ".pom", ".module", "-sources.jar", "-javadoc.jar"):
                name, data = prefix + suffix, b"fixture component"
                entries += [(name, data), (name + ".asc", b"-----BEGIN PGP SIGNATURE-----\nfixture")]
                entries += [(name + "." + algorithm, hashlib.new(algorithm, data).hexdigest().encode())
                            for algorithm in ("md5", "sha1", "sha256", "sha512")]
            output.write_bytes(central.package.zip_bytes(entries))
            central.check_bundle(output)
            with zipfile.ZipFile(output) as archive:
                self.assertEqual(len(archive.namelist()), 30)
            output.write_bytes(central.package.zip_bytes([(name, b"tampered" if name.endswith(".aar") else data)
                                                         for name, data in entries]))
            with self.assertRaisesRegex(ValueError, "checksum"):
                central.check_bundle(output)
            for extra in ("../outside", prefix + ".aar"):
                output.write_bytes(central.package.zip_bytes(entries + [(extra, b"unexpected")]))
                with self.assertRaisesRegex(ValueError, "exactly this component"):
                    central.check_bundle(output)

    def test_prereleases_are_unique_per_run_and_repeatable_on_retry(self):
        self.assertEqual(source_release.prerelease("0.1.0-dev", "123"), "0.1.0-dev.123")
        self.assertEqual(source_release.prerelease("1.2.0", "124"), "1.2.0-dev.124")
        for build in ("", "01", "-1", "123\ninjected=value", None):
            with self.subTest(build=build), self.assertRaises(ValueError):
                source_release.prerelease("0.1.0-dev", build)

    def test_release_source_changes_versions_and_guides_without_changing_code_or_original_design(self):
        with tempfile.TemporaryDirectory(prefix="kylok-source-plan-") as directory:
            root = Path(directory)
            for name in ("README.md", "AGENTS.md", "CONTRIBUTING.md", "ROADMAP.md", "go/README.md"):
                path = root / name
                path.parent.mkdir(exist_ok=True)
                path.write_text("SDK 0.1.0-dev; example 0.1.0-dev.123; go@v0.1.0-dev\n")
            (root / "android").mkdir()
            for sdk in ("go", "android"):
                (root / sdk / "VERSION").write_text("0.1.0-dev\n")
            (root / "design.md").write_text("Preserve 0.1.0-dev\n")
            (root / "docs/validation").mkdir(parents=True)
            (root / "docs/validation/physical.md").write_text("Tested historical 0.1.0-dev\n")
            with patch.object(source_release, "ROOT", root), patch.object(source_release.package, "ROOT", root):
                versions, files = source_release.plan("a" * 40, "456")
            self.assertEqual(versions, {"go": "0.1.0-dev.456", "android": "0.1.0-dev.456"})
            self.assertNotIn("design.md", files)
            self.assertNotIn("docs/validation/physical.md", files)
            self.assertIn("go@v0.1.0-dev.456", files["README.md"])
            self.assertIn("example 0.1.0-dev.123", files["README.md"])
            self.assertEqual(json.loads(files["release/build.json"])["source_commit"], "a" * 40)
            self.assertTrue(all(path.endswith((".md", "/VERSION", ".json")) for path in files))

    def test_existing_sdk_tags_cannot_be_retargeted(self):
        existing = subprocess.CompletedProcess([], 0, json.dumps({"object": {"sha": "a" * 40}}).encode(), b"")
        with patch.object(source_release.subprocess, "run", return_value=existing), patch.object(source_release, "api") as api:
            source_release.ensure_ref("owner/repo", "go/v0.1.0-dev.1", "a" * 40)
            api.assert_not_called()
            with self.assertRaisesRegex(ValueError, "Refusing to move"):
                source_release.ensure_ref("owner/repo", "go/v0.1.0-dev.1", "b" * 40)

    def test_failed_or_lost_upload_response_never_causes_an_automatic_duplicate_upload(self):
        with tempfile.TemporaryDirectory(prefix="kylok-central-retry-") as directory:
            bundle = Path(directory) / "bundle.zip"
            bundle.write_bytes(b"signed fixture")
            saved = []
            with patch.dict(os.environ, {"MAVEN_CENTRAL_USERNAME": "fixture", "MAVEN_CENTRAL_PASSWORD": "fixture"}), \
                 patch.object(central, "check_bundle"), patch.object(central.package, "git", return_value=b"commit\n"), \
                 patch.object(central, "load_checkpoint", return_value=None), \
                 patch.object(central, "checkpoint", side_effect=lambda repo, tag, value: saved.append(dict(value))), \
                 patch.object(central, "request", side_effect=TimeoutError("lost upload response")) as request:
                with self.assertRaises(TimeoutError):
                    central.publish(bundle, "owner/repo", "release/1")
                self.assertEqual(request.call_count, 1)
            self.assertEqual(saved[-1]["stage"], "uploading")
            with patch.dict(os.environ, {"MAVEN_CENTRAL_USERNAME": "fixture", "MAVEN_CENTRAL_PASSWORD": "fixture"}), \
                 patch.object(central, "check_bundle"), patch.object(central.package, "git", return_value=b"commit\n"), \
                 patch.object(central, "load_checkpoint", return_value=saved[-1]), patch.object(central, "request") as request:
                with self.assertRaisesRegex(ValueError, "Upload result unknown"):
                    central.publish(bundle, "owner/repo", "release/1")
                request.assert_not_called()

    def test_published_deployment_must_match_the_expected_maven_component(self):
        with tempfile.TemporaryDirectory(prefix="kylok-central-component-") as directory:
            bundle = Path(directory) / "bundle.zip"
            bundle.write_bytes(b"signed fixture")
            state = {"commit": "commit", "bundle_sha256": hashlib.sha256(bundle.read_bytes()).hexdigest(),
                     "deployment_id": "portal-id", "stage": "PUBLISHED"}
            with patch.dict(os.environ, {"MAVEN_CENTRAL_USERNAME": "fixture", "MAVEN_CENTRAL_PASSWORD": "fixture"}), \
                 patch.object(central, "check_bundle"), patch.object(central.package, "git", return_value=b"commit\n"), \
                 patch.object(central, "load_checkpoint", return_value=state), \
                 patch.object(central, "request", return_value=b'{"deploymentState":"PUBLISHED","purls":["pkg:maven/other/app@1"]}'):
                with self.assertRaisesRegex(ValueError, "different Maven component"):
                    central.publish(bundle, "owner/repo", "release/1")


class ReleaseGates(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="kylok-release-gates-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        for sdk in ("go", "android"):
            (self.root / sdk).mkdir()
            (self.root / sdk / "VERSION").write_text("0.1.0-dev\n")
        (self.root / "ROADMAP.md").write_text("## v1 release acceptance\n" + "- [ ] **Pending:** physical evidence\n" * 13)
        self.addCleanup(patch.stopall)
        patch.object(release, "ROOT", self.root).start()
        patch.object(release.package, "ROOT", self.root).start()

    def test_independent_matching_tag_and_invalid_or_stale_tags(self):
        self.assertEqual(release.validate("go/v0.1.0-dev", clean=False), ("go", "0.1.0-dev"))
        self.assertEqual(release.validate("android/v0.1.0-dev", clean=False), ("android", "0.1.0-dev"))
        (self.root / "android/VERSION").write_text("2.0.0-dev\n")
        self.assertEqual(release.validate("android/v2.0.0-dev", clean=False), ("android", "2.0.0-dev"))
        self.assertEqual(release.validate("go/v0.1.0-dev", clean=False), ("go", "0.1.0-dev"))
        (self.root / "go/VERSION").write_text("2.0.0-dev\n")
        with self.assertRaisesRegex(ValueError, "new module path"):
            release.validate("go/v2.0.0-dev", clean=False)
        (self.root / "go/VERSION").write_text("0.1.0-dev\n")
        for tag in ("v0.1.0-dev", "go/v0.1.1", "android/../../main", "go/v0.1.0-dev\nversion=injected"):
            with self.subTest(tag=tag), self.assertRaises(ValueError):
                release.validate(tag, clean=False)

    def test_stable_v1_cannot_skip_physical_and_release_acceptance(self):
        (self.root / "go/VERSION").write_text("1.0.0\n")
        with self.assertRaisesRegex(ValueError, "thirteen release gates"):
            release.validate("go/v1.0.0", clean=False)
        (self.root / "android/VERSION").write_text("2.0.0\n")
        with self.assertRaisesRegex(ValueError, "thirteen release gates"):
            release.validate("android/v2.0.0", clean=False)
        (self.root / "ROADMAP.md").write_text("## v1 release acceptance\n" + "- [x] **Verified:** evidence\n" * 13)
        self.assertEqual(release.validate("go/v1.0.0", clean=False), ("go", "1.0.0"))

    def test_dirty_or_different_tag_source_is_rejected(self):
        with patch.object(release.package, "git", return_value=b" M go/file.go\n"):
            with self.assertRaisesRegex(ValueError, "clean committed"):
                release.validate("go/v0.1.0-dev")
        with patch.object(release.package, "git", side_effect=[b"", b"commit-one\n", b"commit-two\n"]):
            with self.assertRaisesRegex(ValueError, "exact existing"):
                release.validate("go/v0.1.0-dev")
        with patch.object(release.package, "git", side_effect=[b"", b"same\n", b"same\n"]):
            self.assertEqual(release.validate("go/v0.1.0-dev"), ("go", "0.1.0-dev"))

    def test_distribution_identity_must_match_clean_source(self):
        (self.root / "dist").mkdir()
        (self.root / "dist/kylok-release.json").write_text(json.dumps({"dirty": True, "git_commit": "same"}))
        with patch.object(release.package, "git", side_effect=[b"", b"same\n", b"same\n"]):
            with self.assertRaisesRegex(ValueError, "dirty or belongs"):
                release.validate("go/v0.1.0-dev", distribution=self.root / "dist")


class RealDistribution(unittest.TestCase):
    def test_rebuilt_maven_bytes_must_match_prepared_distribution(self):
        source = ROOT / "out/release/maven"
        with tempfile.TemporaryDirectory(prefix="kylok-maven-compare-") as temp:
            rebuilt = Path(temp) / "maven"
            shutil.copytree(source, rebuilt)
            distribution.compare_maven(source, rebuilt)
            sources = next(rebuilt.rglob("*-sources.jar"))
            sources.write_bytes(sources.read_bytes() + b"changed-source")
            with self.assertRaisesRegex(ValueError, "differs from the verified"):
                distribution.compare_maven(source, rebuilt)
            with self.assertRaisesRegex(ValueError, "No prepared"):
                distribution.compare_maven(Path(temp) / "absent", rebuilt)

    def test_tampered_aar_and_unexpected_files_are_rejected(self):
        source = ROOT / "out/release"
        distribution.check(source)
        with tempfile.TemporaryDirectory(prefix="kylok-distribution-tamper-") as temp:
            copied = Path(temp) / "distribution"
            shutil.copytree(source, copied)
            aar = next((copied / "maven").rglob("*.aar"))
            original = aar.read_bytes()
            aar.write_bytes(original + b"tampered")
            with self.assertRaises(AssertionError):
                distribution.check(copied)
            aar.write_bytes(original)
            (copied / "unexpected.key").write_text("fixture-only")
            with self.assertRaises(AssertionError):
                distribution.check(copied)


if __name__ == "__main__":
    unittest.main()
