#!/usr/bin/env python3
# SPDX-License-Identifier: LGPL-3.0-only
import os
import subprocess
import sys
import xml.etree.ElementTree as ET

TARGET_THRESHOLD = 95.0
EXCLUDED_PATTERNS = ('BuildConfig', 'R$', 'R', 'proto/', '_Impl')

def is_jdk17_or_higher(path):
    """Check if the given JDK path is Java 17 or higher."""
    javac = os.path.join(path, 'bin', 'javac')
    if not os.path.exists(javac):
        return False
    try:
        out = subprocess.check_output([javac, '-version'], stderr=subprocess.STDOUT, text=True)
        version_parts = out.strip().split()
        version_str = version_parts[1] if len(version_parts) > 1 else ""
        major = int(version_str.split('.')[0])
        return major >= 17
    except Exception:
        return False

def find_jdk17():
    """Locate a Java 17 installation for Gradle daemon execution."""
    java_home = os.environ.get('JAVA_HOME')
    if java_home and is_jdk17_or_higher(java_home):
        return java_home

    known_paths = [
        '/opt/homebrew/Cellar/openjdk@17/17.0.20.1/libexec/openjdk.jdk/Contents/Home',
        '/opt/homebrew/opt/openjdk@17/libexec/openjdk.jdk/Contents/Home',
        '/Library/Java/JavaVirtualMachines/openjdk-17.jdk/Contents/Home',
        '/usr/lib/jvm/java-17-openjdk',
    ]
    for p in known_paths:
        if is_jdk17_or_higher(p):
            return p

    try:
        out = subprocess.check_output(['/usr/libexec/java_home', '-v', '17'], text=True).strip()
        if out and is_jdk17_or_higher(out):
            return out
    except Exception:
        pass

    return None

def run_tests_and_coverage():
    """Execute Android unit tests with Jacoco coverage and return the generated XML report path."""
    repo_root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    report_xml = os.path.join(
        repo_root, 'android', 'kylok-dpc', 'build', 'reports', 'coverage', 'test', 'debug', 'report.xml'
    )
    gradlew = os.path.join(repo_root, 'android', 'gradlew')

    env = dict(os.environ)
    jdk17 = find_jdk17()
    if jdk17:
        env['JAVA_HOME'] = jdk17

    cmd = [
        gradlew, '-p', 'android',
        ':kylok-dpc:testDebugUnitTest',
        ':kylok-dpc:createDebugUnitTestCoverageReport'
    ]
    print(f"Running Android tests with Jacoco coverage: {' '.join(cmd)}")
    result = subprocess.run(cmd, cwd=repo_root, env=env, capture_output=True, text=True)
    if result.returncode != 0:
        print("Android tests or coverage generation failed:\n", result.stderr or result.stdout)
        sys.exit(result.returncode)

    if not os.path.exists(report_xml):
        print(f"Coverage report XML not found at {report_xml}")
        sys.exit(1)

    return report_xml

def calculate_coverage(report_xml):
    """Parse Jacoco XML instruction counters, filtering out generated protobuf and Room artifacts."""
    tree = ET.parse(report_xml)
    root = tree.getroot()

    classes = []
    total_missed = 0
    total_covered = 0

    for package in root.findall('package'):
        pkg_name = package.get('name', '')
        if not pkg_name.startswith('io/github/thienvu18/kylok/dpc'):
            continue

        for cls in package.findall('class'):
            cname = cls.get('name', '')
            if any(pattern in cname for pattern in EXCLUDED_PATTERNS):
                continue

            c_missed = 0
            c_covered = 0
            for counter in cls.findall('counter'):
                if counter.get('type') == 'INSTRUCTION':
                    c_missed = int(counter.get('missed', 0))
                    c_covered = int(counter.get('covered', 0))
                    break

            total = c_missed + c_covered
            if total > 0:
                pct = (c_covered / total * 100.0)
                classes.append((c_missed, c_covered, total, pct, cname))
                total_missed += c_missed
                total_covered += c_covered

    total_instr = total_missed + total_covered
    overall_pct = (total_covered / total_instr * 100.0) if total_instr > 0 else 0.0

    print("\nAndroid class instruction coverage breakdown (sorted by missed instructions):")
    classes.sort(key=lambda item: item[0], reverse=True)
    for missed, covered, total, pct, name in classes:
        simple_name = name.split('/')[-1]
        print(f"  {simple_name:40s} {covered:5d}/{total:5d} instructions ({pct:6.2f}%, {missed:4d} missed)")

    print(f"\nOverall Android DPC instruction coverage: {total_covered}/{total_instr} ({overall_pct:.2f}%)")
    return overall_pct

def main():
    """Run Android coverage check and enforce instruction coverage threshold >= 95.0%."""
    report_xml = run_tests_and_coverage()
    pct = calculate_coverage(report_xml)
    if pct < TARGET_THRESHOLD:
        print(f"\nERROR: Android instruction coverage {pct:.2f}% is below target gate {TARGET_THRESHOLD:.1f}%")
        sys.exit(1)
    print(f"\nPASS: Android instruction coverage {pct:.2f}% satisfies target gate >= {TARGET_THRESHOLD:.1f}%")

if __name__ == '__main__':
    main()
