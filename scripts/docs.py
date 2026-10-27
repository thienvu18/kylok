#!/usr/bin/env python3
"""Generate the bilingual site and Go reference from current documented source."""
# SPDX-License-Identifier: LGPL-3.0-only
import argparse
import ast
import html
from html.parser import HTMLParser
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
from urllib.parse import unquote, urlsplit

ROOT = Path(__file__).resolve().parents[1]
BASE = "https://thienvu18.github.io/kylok"
PAGES = ["index", "install", "examples", "architecture", "policies", "development", "releases", "compatibility", "roadmap", "api"]
LABELS = {
    "en": ["Introduction", "Install & integrate", "Example applications", "Architecture & transport", "Policies & kiosk", "Development", "Release packages", "Compatibility", "Roadmap", "API reference"],
    "vi": ["Giới thiệu", "Cài đặt và tích hợp", "Ứng dụng mẫu", "Kiến trúc và kết nối", "Chính sách và kiosk", "Phát triển", "Gói phát hành", "Tương thích", "Lộ trình", "Tham chiếu API"],
}


def go_index():
    """Read declarations through Go's parser so signatures and comments track the actual SDK."""
    data = subprocess.check_output(["go", "run", "./cmd/doc-index", "-root", ".."], cwd=ROOT / "go", env={**os.environ, "GOWORK": "off"})
    return json.loads(data)


def audit(records):
    """Reject missing method purposes and supported public usage docs before generating the reference."""
    errors = []
    for item in records:
        if item["kind"] != "method":
            continue
        location = f"{item['file']}:{item['line']} {item['name']}"
        if not item["doc"].strip():
            errors.append(location + ": purpose comment required")
        elif item["public"] and "Usage:" not in item["doc"]:
            errors.append(location + ": public usage documentation required")
        if re.search(r"\b(?:M[0-6]|Phase\s+\d+)\b", item["doc"]):
            errors.append(location + ": explain purpose instead of a project phase")
    for folder in ("android/kylok-dpc/src/main", "examples/minimal-dpc/src/main", "examples/configuration-app/src/main"):
        for path in (ROOT / folder).rglob("*.kt"):
            lines = path.read_text().splitlines()
            internal = bool(re.search(r"^internal (?:object|class|interface|enum|data)", path.read_text(), re.M))
            for index, line in enumerate(lines):
                if not re.search(r"\bfun\s+(?:\w+\.)?\w+\s*\(", line):
                    continue
                before = index - 1
                while before >= 0 and not lines[before].strip():
                    before -= 1
                comment = []
                if before >= 0 and lines[before].strip().endswith("*/"):
                    while before >= 0:
                        comment.insert(0, lines[before])
                        if "/**" in lines[before]:
                            break
                        before -= 1
                value = "\n".join(comment)
                location = f"{path.relative_to(ROOT)}:{index + 1}"
                if not value:
                    errors.append(location + ": method purpose KDoc required")
                host_method = not internal and not re.search(r"\b(private|internal|protected)\b", line)
                if host_method and "Usage:" not in value:
                    errors.append(location + ": public usage/callback documentation required")
                if re.search(r"\b(?:M[0-6]|Phase\s+\d+)\b", value):
                    errors.append(location + ": explain purpose instead of a project phase")
    for path in (ROOT / "scripts").glob("*.py"):
        if path.name.startswith("test-"):
            continue
        for node in ast.walk(ast.parse(path.read_text())):
            if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)) and not ast.get_docstring(node):
                errors.append(f"{path.relative_to(ROOT)}:{node.lineno}: function purpose docstring required")
    if errors:
        raise ValueError("\n".join(errors))


def reference(records):
    """Render the supported Go facade/adapters/contracts, retaining exact signatures and source locations."""
    lines = ["# Go API reference", "", "Generated from Go declarations and purpose/usage comments. Development contracts may change before v1.", ""]
    public = [r for r in records if r["public"] and not any(x in r["file"] for x in ("/cmd/", "/internal/")) and not r["file"].startswith("examples/")]
    for item in sorted(public, key=lambda r: (r["file"], r["line"])):
        receiver = item["name"].split(".")[0].lstrip("*")
        if "." in item["name"] and not receiver[:1].isupper():
            continue
        lines += [f"## {item['name']}", "", f"`{item['file']}:{item['line']}`", "", "```go", item["signature"], "```", "", item["doc"].strip(), ""]
    return "\n".join(lines)


def render_markdown(text):
    """Use a pinned Markdown renderer for code, tables and anchors rather than ad-hoc HTML substitution."""
    import markdown
    return markdown.markdown(text, extensions=["fenced_code", "tables", "toc", "sane_lists"])


def page_html(language, name, title, body, shared_reference=False):
    """Keep navigation, language switching and canonical metadata consistent across every localized page."""
    other = "vi" if language == "en" else "en"
    prefix = "../en/" if shared_reference else ""
    selected = ' aria-current="page"'
    nav = "".join(f'<a href="{prefix}{slug}.html"{selected if slug == name else ""}>{html.escape(label)}</a>' for slug, label in zip(PAGES, LABELS[language]))
    canonical = f"{BASE}/api/go.html" if shared_reference else f"{BASE}/{language}/{name}.html"
    description = "Open-source Go and Kotlin SDKs for adding Android enrollment, APK delivery and observable device policies to your own self-hosted product." if language == "en" else "Bộ SDK mã nguồn mở Go và Kotlin giúp tích hợp đăng ký thiết bị Android, phân phối APK và theo dõi chính sách trên hạ tầng do bạn vận hành."
    return f'''<!doctype html><html lang="{language}"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{html.escape(title)} · Kylok</title><meta name="description" content="{html.escape(description)}"><link rel="canonical" href="{canonical}">
<link rel="alternate" hreflang="en" href="{BASE}/en/{name}.html"><link rel="alternate" hreflang="vi" href="{BASE}/vi/{name}.html">
<meta property="og:title" content="{html.escape(title)} · Kylok"><meta property="og:description" content="{html.escape(description)}"><meta property="og:type" content="website">
<link rel="stylesheet" href="../assets/site.css"><script src="../assets/site.js" defer></script></head><body>
<header><a class="brand" href="{prefix}index.html">kylok<span>Go + Android SDK</span></a><nav aria-label="Language and source"><a href="../{other}/{name}.html" lang="{other}">{'Tiếng Việt' if other == 'vi' else 'English'}</a><a href="https://github.com/thienvu18/kylok">GitHub ↗</a></nav></header>
<div class="layout"><aside><label for="search">{'Find a topic' if language == 'en' else 'Tìm chủ đề'}</label><input id="search" type="search" placeholder="{'Search documentation' if language == 'en' else 'Tìm trong tài liệu'}" aria-controls="search-results"><div id="search-results" role="status"></div><nav aria-label="Documentation">{nav}</nav><p class="small">LGPL-3.0-only<br>{'Experimental · development SDKs' if language == 'en' else 'Thử nghiệm · SDK phát triển'}</p></aside>
<main id="content">{body}</main></div><footer>Kylok · {'Your product. Your infrastructure.' if language == 'en' else 'Sản phẩm của bạn. Hạ tầng của bạn.'} · <a href="{prefix}compatibility.html">{'Verification evidence' if language == 'en' else 'Bằng chứng kiểm tra'}</a></footer></body></html>'''


def build(output):
    """Build only generated site output while preserving authored Markdown and the original design draft."""
    records = go_index()
    audit(records)
    output = output.resolve()
    if output != ROOT / "out/site":
        raise ValueError("Site output must be the repository's generated out/site directory")
    if output.exists():
        shutil.rmtree(output)
    output.mkdir(parents=True)
    shutil.copytree(ROOT / "website/assets", output / "assets")
    search = []
    for language in ("en", "vi"):
        folder = output / language
        folder.mkdir()
        for name, title in zip(PAGES, LABELS[language]):
            source = ROOT / "website" / language / (name + ".md")
            if not source.is_file():
                raise ValueError(f"Missing localized documentation: {source}")
            text = source.read_text()
            body = render_markdown(text)
            (folder / (name + ".html")).write_text(page_html(language, name, title, body))
            search.append({"lang": language, "title": title, "url": f"{language}/{name}.html", "text": re.sub(r"<[^>]+>", " ", body)})
    api = output / "api"
    api.mkdir()
    (api / "go.html").write_text(page_html("en", "api", "Go API reference", render_markdown(reference(records)), shared_reference=True))
    native = ROOT / "android/kylok-dpc/build/dokka/html"
    if not (native / "index.html").is_file():
        raise ValueError("Generate Android API documentation with the Dokka task first")
    shutil.copytree(native, api / "android")
    (output / "search.json").write_text(json.dumps(search, ensure_ascii=False))
    (output / "index.html").write_text('<!doctype html><html lang="en"><meta charset="utf-8"><meta http-equiv="refresh" content="0;url=en/index.html"><title>Kylok SDK</title><a href="en/index.html">English</a> · <a href="vi/index.html">Tiếng Việt</a></html>')
    urls = [f"{BASE}/{language}/{name}.html" for language in ("en", "vi") for name in PAGES]
    (output / "sitemap.xml").write_text('<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">' + "".join(f"<url><loc>{u}</loc></url>" for u in urls) + "</urlset>")
    (output / "robots.txt").write_text(f"User-agent: *\nAllow: /\nSitemap: {BASE}/sitemap.xml\n")
    (output / ".nojekyll").write_text("")
    check(output)
    print(f"Generated bilingual documentation and {len(records)} indexed Go declarations in {output}")


class Links(HTMLParser):
    """Collect local resource/navigation links for a static-site completeness check."""
    def __init__(self):
        """Start an empty resource list for one rendered document."""
        super().__init__()
        self.links = []

    def handle_starttag(self, tag, attrs):
        """Record actual href/src targets without treating code samples as links."""
        for key, value in attrs:
            if key in ("href", "src") and value:
                self.links.append(value)


def check(output):
    """Verify translated page pairs and local site assets; Dokka's external reference links stay external."""
    errors = []
    for language in ("en", "vi"):
        for name in PAGES:
            if not (output / language / (name + ".html")).is_file():
                errors.append(f"Missing {language}/{name}.html")
    for path in list((output / "en").glob("*.html")) + list((output / "vi").glob("*.html")) + list((output / "api").glob("*.html")):
        parser = Links()
        parser.feed(path.read_text())
        for target in parser.links:
            url = urlsplit(target)
            if url.scheme or url.netloc or not url.path:
                continue
            resolved = (path.parent / unquote(url.path)).resolve()
            if not resolved.is_relative_to(output.resolve()) or not resolved.exists():
                errors.append(f"{path.relative_to(output)}: broken link {target}")
    if errors:
        raise ValueError("\n".join(errors))


def main():
    """Expose explicit build/audit commands without installing dependencies or publishing automatically."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=["build", "audit", "check"])
    args = parser.parse_args()
    try:
        if args.command == "audit":
            records = go_index(); audit(records); print(f"Checked {len(records)} Go declarations, Kotlin methods and maintainer Python functions")
        elif args.command == "check":
            check(ROOT / "out/site")
        else:
            build(ROOT / "out/site")
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        parser.exit(1, str(error) + "\n")


if __name__ == "__main__":
    main()
