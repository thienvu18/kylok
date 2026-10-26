#!/usr/bin/env python3
"""Sign Maven artifacts and publish a resumable Central Portal deployment."""
# SPDX-License-Identifier: LGPL-3.0-only
import argparse
import base64
import hashlib
from importlib.util import module_from_spec, spec_from_file_location
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
import zipfile

ROOT = Path(__file__).resolve().parents[1]
spec = spec_from_file_location("kylok_package", ROOT / "scripts/package-release.py")
package = module_from_spec(spec)
spec.loader.exec_module(package)
API = "https://central.sonatype.com/api/v1/publisher"


def signing_files(repository):
    """Select the current immutable component, excluding mutable repository-level Maven metadata."""
    version = package.version("android")
    folder = Path(repository) / package.GROUP_PATH / version
    base = "kylok-dpc-" + version
    files = [folder / (base + suffix) for suffix in (".aar", ".pom", ".module", "-sources.jar", "-javadoc.jar")]
    if not all(path.is_file() and not path.is_symlink() for path in files):
        raise ValueError("Build the complete local Maven publication before signing")
    return files


def gpg(home, *args, data=None):
    """Run GnuPG in an isolated keyring; pass secrets through stdin instead of command arguments."""
    result = subprocess.run(["gpg", "--homedir", str(home), "--batch", "--yes", *args], input=data, capture_output=True)
    if result.returncode:
        raise ValueError("GPG signing/verification failed; check the signing key, passphrase and expiry")
    return result.stdout


def bundle(repository, output):
    """Package real KDoc, sources, PGP signatures and required checksums in Central's repository layout."""
    key, password = os.environ.get("MAVEN_SIGNING_KEY"), os.environ.get("MAVEN_SIGNING_PASSWORD")
    if not key or not password:
        raise ValueError("Set MAVEN_SIGNING_KEY and MAVEN_SIGNING_PASSWORD")
    entries = []
    with tempfile.TemporaryDirectory(prefix="kylok-pgp-") as directory:
        home = Path(directory)
        try:
            gpg(home, "--import", data=key.encode())
            listing = gpg(home, "--with-colons", "--list-secret-keys").decode()
            fingerprints = [line.split(":")[9] for line in listing.splitlines() if line.startswith("fpr:")]
            if not fingerprints:
                raise ValueError("No signing key was imported")
            fingerprint = fingerprints[0]
            expected = os.environ.get("MAVEN_SIGNING_FINGERPRINT")
            if expected and fingerprint != expected:
                raise ValueError("Signing key differs from the configured release identity")
            for path in signing_files(repository):
                name, data = path.relative_to(repository).as_posix(), path.read_bytes()
                signature = home / "signature.asc"
                gpg(home, "--pinentry-mode", "loopback", "--passphrase-fd", "0", "--local-user", fingerprint,
                    "--digest-algo", "SHA256", "--armor", "--detach-sign", "--output", str(signature), str(path),
                    data=password.encode() + b"\n")
                gpg(home, "--verify", str(signature), str(path))
                entries += [(name, data), (name + ".asc", signature.read_bytes())]
                for algorithm in ("md5", "sha1", "sha256", "sha512"):
                    entries.append((name + "." + algorithm, hashlib.new(algorithm, data).hexdigest().encode()))
        finally:
            subprocess.run(["gpgconf", "--homedir", str(home), "--kill", "gpg-agent"], capture_output=True)
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_bytes(package.zip_bytes(entries))
    check_bundle(output)
    print("Signed Central bundle:", output, "fingerprint:", fingerprint)


def check_bundle(path):
    """Reject extra/traversing/duplicate paths, missing signatures and corrupted component checksums."""
    prefix = (package.GROUP_PATH / package.version("android")).as_posix() + "/kylok-dpc-" + package.version("android")
    expected = {prefix + suffix for suffix in (".aar", ".pom", ".module", "-sources.jar", "-javadoc.jar")}
    with zipfile.ZipFile(path) as archive:
        names = archive.namelist()
        complete = {name + suffix for name in expected for suffix in ("", ".asc", ".md5", ".sha1", ".sha256", ".sha512")}
        if len(names) != len(set(names)) or set(names) != complete:
            raise ValueError("Central bundle must contain exactly this component and its signatures/checksums")
        for name in expected:
            data = archive.read(name)
            if not archive.read(name + ".asc").startswith(b"-----BEGIN PGP SIGNATURE-----"):
                raise ValueError("Missing armored PGP signature")
            for algorithm in ("md5", "sha1", "sha256", "sha512"):
                if archive.read(name + "." + algorithm).decode().strip() != hashlib.new(algorithm, data).hexdigest():
                    raise ValueError("Invalid Central bundle checksum")
    print("Central bundle layout and checksums verified")


class NoRedirects(urllib.request.HTTPRedirectHandler):
    """Keep the publishing bearer credential on the Portal HTTPS origin."""
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        """Reject redirects rather than forwarding publishing credentials."""
        raise ValueError("Central Portal redirects are not permitted")


def request(route, token, data=b"", content_type=None):
    """Make a bounded Portal POST without logging authentication or retrying an ambiguous upload."""
    headers = {"Authorization": "Bearer " + token}
    if content_type:
        headers["Content-Type"] = content_type
    req = urllib.request.Request(API + route, data=data, headers=headers, method="POST")
    try:
        with urllib.request.build_opener(NoRedirects()).open(req, timeout=60) as response:
            return response.read(2 * 1024 * 1024)
    except urllib.error.HTTPError as error:
        raise ValueError(f"Central Portal HTTP {error.code}; deployment checkpoint retained") from None


def checkpoint(repo, tag, state):
    """Persist deployment identity on the draft before another irreversible publication effect."""
    output = ROOT / "out/central-deployment.json"
    output.parent.mkdir(exist_ok=True)
    output.write_text(json.dumps(state, indent=2, sort_keys=True) + "\n")
    result = subprocess.run(["gh", "release", "upload", tag, str(output), "--repo", repo, "--clobber"], capture_output=True)
    if result.returncode:
        raise ValueError("Cannot persist Central deployment checkpoint on the release draft")


def load_checkpoint(repo, tag):
    """Recover the upload ID so retries never upload an already submitted immutable version again."""
    result = subprocess.run(["gh", "release", "view", tag, "--repo", repo, "--json", "assets"], capture_output=True)
    if result.returncode:
        raise ValueError("Create the GitHub draft before publishing to Central")
    if "central-deployment.json" not in {item["name"] for item in json.loads(result.stdout)["assets"]}:
        return None
    with tempfile.TemporaryDirectory(prefix="kylok-central-state-") as directory:
        subprocess.run(["gh", "release", "download", tag, "--repo", repo, "--pattern", "central-deployment.json", "--dir", directory], check=True)
        return json.loads((Path(directory) / "central-deployment.json").read_text())


def publish(path, repo, tag, deployment_id=None):
    """Resume a signed deployment until Central confirms publication, without overwriting package versions."""
    check_bundle(path)
    username, password = os.environ.get("MAVEN_CENTRAL_USERNAME"), os.environ.get("MAVEN_CENTRAL_PASSWORD")
    if not username or not password:
        raise ValueError("Set the two Central Portal user-token secrets")
    token = base64.b64encode((username + ":" + password).encode()).decode()
    commit = package.git("rev-parse", "HEAD").decode().strip()
    digest = hashlib.sha256(path.read_bytes()).hexdigest()
    state = load_checkpoint(repo, tag) or {"format": 1, "commit": commit, "bundle_sha256": digest, "stage": "prepared"}
    if state["commit"] != commit or state["bundle_sha256"] != digest:
        raise ValueError("Prepared bundle/source differs from the persisted deployment")
    if deployment_id:
        # A lost upload response requires the actual Portal ID, never a blind second upload.
        state["deployment_id"] = str(uuid.UUID(deployment_id))
    if not state.get("deployment_id"):
        if state["stage"] != "prepared":
            raise ValueError("Upload result unknown: recover the deployment ID from Central Portal before retrying")
        state["stage"] = "uploading"
        checkpoint(repo, tag, state)
        boundary = "kylok-" + uuid.uuid4().hex
        body = (f'--{boundary}\r\nContent-Disposition: form-data; name="bundle"; filename="central-bundle.zip"\r\n'
                'Content-Type: application/octet-stream\r\n\r\n').encode() + path.read_bytes() + f"\r\n--{boundary}--\r\n".encode()
        query = urllib.parse.urlencode({"publishingType": "AUTOMATIC", "name": repo + " " + tag})
        state["deployment_id"] = str(uuid.UUID(request("/upload?" + query, token, body, "multipart/form-data; boundary=" + boundary).decode().strip()))
        state["stage"] = "uploaded"
        checkpoint(repo, tag, state)
    for _ in range(150):  # Wait up to 25 minutes; reruns continue with the persisted Portal ID.
        response = json.loads(request("/status?" + urllib.parse.urlencode({"id": state["deployment_id"]}), token))
        stage = response["deploymentState"]
        if stage != state["stage"]:
            state["stage"] = stage
            checkpoint(repo, tag, state)
            print("Central deployment:", state["deployment_id"], stage, flush=True)
        if stage == "PUBLISHED":
            expected = "pkg:maven/io.github.thienvu18.kylok/kylok-dpc@" + package.version("android")
            if expected not in response.get("purls", []):
                raise ValueError("Portal deployment belongs to a different Maven component/version")
            return
        if stage == "FAILED":
            raise ValueError("Central rejected this deployment: " + json.dumps(response.get("errors", {})))
        # AUTOMATIC deployments advance from validation themselves; a second publish request can race them.
        time.sleep(10)
    raise ValueError("Central is still processing; rerun to resume the same deployment ID")


def main():
    """Expose local signing/checking and explicit remote publication separately from PR tests."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=["bundle", "check", "publish"])
    parser.add_argument("--repository", type=Path, default=ROOT / "out/release/maven")
    parser.add_argument("--bundle", type=Path, default=ROOT / "out/central-bundle.zip")
    parser.add_argument("--repo", default=os.environ.get("GITHUB_REPOSITORY", "thienvu18/kylok"))
    parser.add_argument("--tag")
    parser.add_argument("--deployment-id")
    args = parser.parse_args()
    try:
        if args.command == "bundle":
            bundle(args.repository, args.bundle)
        elif args.command == "check":
            check_bundle(args.bundle)
        else:
            if not args.tag:
                raise ValueError("Publication requires the prepared release tag")
            publish(args.bundle, args.repo, args.tag, args.deployment_id)
    except (ValueError, OSError, KeyError, subprocess.CalledProcessError) as error:
        parser.exit(1, str(error) + "\n")


if __name__ == "__main__":
    main()
