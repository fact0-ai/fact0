#!/usr/bin/env python3
"""Scan only publishable source (including new files), not local operator secrets."""
from pathlib import Path
import argparse
import json
import shutil
import subprocess
import tempfile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--gitleaks", default="gitleaks")
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    files = subprocess.check_output(["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard"], cwd=root).decode().split("\0")
    denied = {".env", "YC_APPLICATION.md", "YC_FORM.md", "STARTUP_SCHOOL_2026_APPLICATION.md", "zfellows_application.md", "gravity.md", "DECISION.md", "PRODUCTION_READINESS_REVIEW.md"}
    with tempfile.TemporaryDirectory(prefix="fact0-release-scan-") as temporary:
        candidate = Path(temporary)
        count = 0
        for name in sorted(set(files)):
            if not name:
                continue
            rel = Path(name)
            if (root / name).is_symlink():
                raise SystemExit(f"Review symlink before publishing: {name}")
            if rel.name in denied or rel.suffix in (".dump", ".backup", ".sqlite", ".sqlite3", ".db", ".rdb") or (rel.name.startswith(".env.") and rel.name != ".env.example") or any(part in ("node_modules", ".venv", ".vercel", ".next", ".git", "research", "backups", ".release-local", ".code-review-graph", ".serena") for part in rel.parts):
                raise SystemExit(f"Private/operator path would be published: {name}")
            if not (root / name).is_file():
                continue
            target = candidate / name
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(root / name, target)
            count += 1
        print(f"Scanning {count} publishable files, excluding ignored local data.", flush=True)
        report_dir = root / ".release-local"
        report_dir.mkdir(exist_ok=True)
        report = report_dir / "source-secrets.json"
        result = subprocess.run([args.gitleaks, "dir", "--redact", "--no-banner", "--report-format", "json", "--report-path", str(report), str(candidate)])
        if result.returncode:
            if report.exists():
                for finding in json.loads(report.read_text()):
                    print(f"Review {finding.get('RuleID')}: {finding.get('File')}:{finding.get('StartLine')}")
            raise SystemExit(result.returncode)


if __name__ == "__main__":
    main()
