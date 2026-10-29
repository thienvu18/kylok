#!/usr/bin/env python3
# SPDX-License-Identifier: LGPL-3.0-only
import os
import subprocess
import sys

TARGET_THRESHOLD = 95.0
SKIPPED_PREFIXES = ('/cmd/', '/gen/', '/ports/', '/internal/testapk/')

def run_tests():
    """Execute Go tests with coverage profiling and return the generated coverage profile path."""
    repo_root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    go_dir = os.path.join(repo_root, 'go')
    cover_profile = os.path.join(go_dir, 'coverage.out')
    
    cmd = [
        'go', 'test', '-count=1',
        '-coverprofile=coverage.out',
        '-coverpkg=github.com/thienvu18/kylok/go/...',
        './...'
    ]
    print(f"Running Go tests with coverage: {' '.join(cmd)}")
    result = subprocess.run(cmd, cwd=go_dir, capture_output=True, text=True)
    if result.returncode != 0:
        print("Go tests failed:\n", result.stderr or result.stdout)
        sys.exit(result.returncode)
        
    return cover_profile

def calculate_coverage(cover_profile):
    """Parse deduplicated Go statement coverage blocks and calculate aggregate and per-package percentages."""
    statements = {}
    with open(cover_profile, 'r', encoding='utf-8') as f:
        for line in f:
            line = line.strip()
            if not line or line.startswith('mode:'):
                continue
            parts = line.split()
            block, num_stmts, count = parts[0], int(parts[1]), int(parts[2])
            if any(skip in block for skip in SKIPPED_PREFIXES):
                continue
            prev_stmts, prev_count = statements.get(block, (num_stmts, 0))
            statements[block] = (num_stmts, prev_count + count)

    total = sum(s[0] for s in statements.values())
    covered = sum(s[0] for s in statements.values() if s[1] > 0)
    pct = (covered / total * 100.0) if total else 0.0

    by_pkg = {}
    for block, (stmts, count) in statements.items():
        file_path = block.split(':')[0]
        pkg = '/'.join(file_path.split('/')[:-1])
        pkg_tot, pkg_cov = by_pkg.get(pkg, (0, 0))
        by_pkg[pkg] = (pkg_tot + stmts, pkg_cov + (stmts if count > 0 else 0))

    print("\nPackage statement coverage breakdown:")
    for pkg in sorted(by_pkg.keys()):
        tot, cov = by_pkg[pkg]
        p = (cov / tot * 100.0) if tot else 0.0
        print(f"  {pkg:50s} {cov:4d}/{tot:4d} ({p:6.2f}%)")

    print(f"\nOverall Go library statement coverage: {covered}/{total} ({pct:.2f}%)")
    return pct

def main():
    """Run coverage check and enforce statement coverage threshold >= 95.0%."""
    cover_profile = run_tests()
    pct = calculate_coverage(cover_profile)
    if pct < TARGET_THRESHOLD:
        print(f"\nERROR: Go statement coverage {pct:.2f}% is below target gate {TARGET_THRESHOLD:.1f}%")
        sys.exit(1)
    print(f"\nPASS: Go statement coverage {pct:.2f}% satisfies target gate >= {TARGET_THRESHOLD:.1f}%")

if __name__ == '__main__':
    main()
