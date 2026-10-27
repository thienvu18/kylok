#!/usr/bin/env python3
"""Check documentation links, release readiness and SDK/example dependency direction."""
# SPDX-License-Identifier: LGPL-3.0-only
from pathlib import Path
import hashlib
import json
import re
import subprocess
from urllib.parse import unquote, urlsplit

ROOT = Path(__file__).resolve().parents[1]
errors = []
docs = [ROOT / name for name in ("README.md", "README.vi.md", "ROADMAP.md", "CONTRIBUTING.md", "AGENTS.md")]
docs += sorted((ROOT / "docs").rglob("*.md"))
docs += [ROOT / "tests/contracts/README.md"]
docs += [ROOT / "go/README.md"]


def visible_markdown(text):
    """Exclude code fences when checking documentation links and heading anchors."""
    return re.sub(r"^```[^\n]*\n.*?^```\s*$", "", text, flags=re.M | re.S)


def anchors(path):
    """Compute Markdown heading anchors, including repeated-heading suffixes."""
    result, counts = set(), {}
    for title in re.findall(r"^#{1,6} (.+)$", visible_markdown(path.read_text()), re.M):
        slug = re.sub(r"[^\w\- ]", "", title.replace("`", "").lower()).replace(" ", "-")
        count = counts.get(slug, 0)
        counts[slug] = count + 1
        result.add(slug if count == 0 else f"{slug}-{count}")
    return result


links = 0
for path in docs:
    text = path.read_text()
    if len(re.findall(r"^# ", text, re.M)) != 1:
        errors.append(f"{path}: expected one H1")
    if len(re.findall(r"^```", text, re.M)) % 2:
        errors.append(f"{path}: unbalanced fences")
    if not text.endswith("\n") or any(line.rstrip() != line for line in text.splitlines()):
        errors.append(f"{path}: missing newline or trailing whitespace")
    for target in re.findall(r"\]\(([^)]+)\)", visible_markdown(text)):
        target = target.strip("<>")
        url = urlsplit(target)
        if url.scheme or url.netloc:
            continue
        links += 1
        linked = (path.parent / unquote(url.path)).resolve() if url.path else path
        if not linked.exists():
            errors.append(f"{path.relative_to(ROOT)}: broken link {target}")
        elif url.fragment and linked.suffix == ".md" and unquote(url.fragment) not in anchors(linked):
            errors.append(f"{path.relative_to(ROOT)}: missing anchor {target}")

roadmap = (ROOT / "ROADMAP.md").read_text()
for document in ("docs/implementation-plan.md", "docs/compatibility.md"):
    if document not in roadmap:
        errors.append(f"Roadmap must link {document}")
readiness = (ROOT / "docs/release-readiness.md").read_text()
gates = re.findall(r"^- \[([ x])\] \*\*([^*]+)\*\*(.*)$", readiness, re.M)
if len(gates) != 13 or len({title for _, title, _ in gates}) != 13:
    errors.append("Stable release readiness must retain thirteen distinct acceptance gates")
if any(state == "x" and not re.search(r"\[[^\]]+\]\([^)]+\)", evidence) for state, _, evidence in gates):
    errors.append("Checked stable release gates require evidence links")
if hashlib.sha256((ROOT / "design.md").read_bytes()).hexdigest() != "a36c0ae3622001ef944f1229bf97d58d3bdde2c098d73c8cdab5763a5c4088ac":
    errors.append("Original design.md changed; preserve the source draft")
if "LGPL-3.0-only" not in (ROOT / "NOTICE").read_text():
    errors.append("Missing license declaration")
for name in ("LICENSE", "COPYING", "NOTICE"):
    if (ROOT / name).read_bytes() != (ROOT / "go" / name).read_bytes():
        errors.append(f"Go module must retain the current root {name}")
for sdk in ("go", "android"):
    version = (ROOT / sdk / "VERSION").read_text().strip()
    if not re.fullmatch(r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?", version) or \
            any(p.isdigit() and len(p) > 1 and p[0] == "0" for p in version.partition("-")[2].split(".")) or \
            sdk == "go" and int(version.split(".", 1)[0]) > 1:
        errors.append(f"Invalid {sdk} SDK version")
android_version = (ROOT / "android/VERSION").read_text().strip()
if f"io.github.thienvu18.kylok:kylok-dpc:{android_version}" not in (ROOT / "docs/sdk.md").read_text():
    errors.append("SDK guide Maven example does not match android/VERSION")
matrix = json.loads((ROOT / "release/compatibility.json").read_text())
if matrix["protocol_version"] != 1 or matrix["pairing"] != "same-source-revision":
    errors.append("Release compatibility must describe the current development protocol/pairing")
if not (ROOT / matrix["physical_evidence"]).is_file():
    errors.append("Release compatibility evidence document is missing")

for module in (ROOT / "go", ROOT / "examples/minimal-server"):
    result = subprocess.run(["go", "list", "-deps", "./..."], cwd=module, text=True, capture_output=True, check=False)
    if result.returncode:
        errors.append(result.stderr)
    elif module.name == "go" and any("/examples/" in p for p in result.stdout.splitlines()):
        errors.append("Go SDK depends on example code")
for path in (ROOT / "android/kylok-dpc/src/main").rglob("*.kt"):
    if re.search(r"^import .*kylok\.example", path.read_text(), re.M):
        errors.append(f"Android SDK depends on example: {path}")

if errors:
    raise SystemExit("\n".join(errors))
print(f"Checked {len(docs)} Markdown documents, {links} local links/anchors, readiness/license consistency and SDK boundaries.")
