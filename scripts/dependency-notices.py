#!/usr/bin/env python3
"""Collect notices for the API/migrator and Claude Code collector binaries."""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess

ROOT = Path(__file__).resolve().parents[1]
API_TARGETS = ("linux/amd64", "linux/arm64")
COLLECTOR_TARGETS = ("darwin/arm64", "darwin/amd64", "linux/arm64", "linux/amd64")
FIRST_PARTY_MODULES = {"github.com/fact0-ai/fact0/sdk/go": ROOT / "sdk/go"}
NOTICE_NAME = re.compile(r"^(?:LICEN[CS]E|COPYING|NOTICE|PATENTS?)(?:$|[._-])", re.I)
SOURCE_SUFFIXES = {".go", ".c", ".h", ".cc", ".cpp", ".py", ".js", ".ts", ".rs"}


def ancestors(folder, root):
    """Only inspect the compiled package's ancestors, within its dependency."""
    folder, root = folder.resolve(), root.resolve()
    if not folder.is_relative_to(root):
        raise SystemExit(f"Package directory {folder} is outside dependency {root}")
    while True:
        yield folder
        if folder == root:
            return
        folder = folder.parent


def notice_files(folders):
    # Do not mistake Go files such as pgproto3/notice_response.go for notices.
    return {
        path
        for folder in folders
        for path in folder.iterdir()
        if path.is_file()
        and NOTICE_NAME.match(path.name)
        and path.suffix.lower() not in SOURCE_SUFFIXES
    }


def packages(working_directory, binaries, target):
    operating_system, architecture = target.split("/")
    env = dict(os.environ, GOOS=operating_system, GOARCH=architecture, CGO_ENABLED="0")
    raw = subprocess.check_output(
        ["go", "list", "-deps", "-json", *binaries],
        cwd=working_directory, env=env, text=True,
    )
    decoder = json.JSONDecoder()
    position = 0
    while position < len(raw):
        while position < len(raw) and raw[position].isspace():
            position += 1
        if position == len(raw):
            return
        item, position = decoder.raw_decode(raw, position)
        yield item


def generate(working_directory, binaries, targets, description, output):
    go_root = Path(subprocess.check_output(
        ["go", "env", "GOROOT"], cwd=working_directory, text=True,
    ).strip()).resolve()
    modules = {}
    first_party = set()
    standard_dirs = {go_root}
    for target in targets:
        for item in packages(working_directory, binaries, target):
            if item.get("Standard") and item.get("Dir"):
                standard_dirs.update(ancestors(Path(item["Dir"]), go_root))
            module = item.get("Module", {})
            if module.get("Dir") and not module.get("Main"):
                name = module["Path"]
                first_party_root = FIRST_PARTY_MODULES.get(name)
                if first_party_root and Path(module["Dir"]).resolve() == first_party_root.resolve():
                    first_party.add(name)
                    continue
                entry = modules.setdefault(name, {"module": module, "dirs": set()})
                if module.get("Version") != entry["module"].get("Version"):
                    raise SystemExit(f"Dependency version differs across targets: {name}")
                entry["dirs"].update(ancestors(Path(item["Dir"]), Path(module["Dir"])))

    go_license = next((p for p in (go_root / "LICENSE", go_root.parent / "LICENSE") if p.is_file()), None)
    if go_license is None:
        raise SystemExit("Go distribution LICENSE not found")
    standard_files = notice_files(standard_dirs) | {go_license}
    # Some distributions place these beside GOROOT rather than inside it.
    for filename in ("PATENTS", "NOTICE"):
        if (go_root.parent / filename).is_file():
            standard_files.add(go_root.parent / filename)

    notices = [
        f"Third-party notices for the {description}.\n"
        f"Targets: {' and '.join(targets)}, CGO_ENABLED=0.\n"
        "Regenerate with python3 scripts/dependency-notices.py after Go dependencies change.\n",
    ]
    for name in sorted(first_party):
        notices.append(f"First-party module {name} is covered by the distributed Fact0 MIT LICENSE.\n")
    notices.append("Go standard library and bundled dependencies\n")
    for path in sorted(standard_files):
        label = str(path.relative_to(go_root)) if path.is_relative_to(go_root) else path.name
        notices.append(label + "\n" + path.read_text(encoding="utf-8"))

    missing = []
    for name, entry in sorted(modules.items()):
        module = entry["module"]
        folder = Path(module["Dir"]).resolve()
        files = notice_files(entry["dirs"])
        if not any(re.match(r"^(?:LICEN[CS]E|COPYING)(?:$|[._-])", p.name, re.I) for p in files):
            missing.append(name)
            continue
        notices.append("\n" + "=" * 72 + "\n" + name + " " + module.get("Version", "") + "\n")
        for path in sorted(files):
            notices.append(str(path.relative_to(folder)) + "\n" + path.read_text(encoding="utf-8"))
    if missing:
        raise SystemExit("Missing module licenses; review before release: " + ", ".join(missing))
    output.write_text("\n".join(notices), encoding="utf-8")
    print(f"Collected {len(modules)} external dependency notices and Go distribution notices for {description}.")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--component", choices=("all", "api", "collector"), default="all")
    args = parser.parse_args()
    if args.component in ("all", "api"):
        generate(ROOT / "api", ("./fact0", "./fact0-migrate"), API_TARGETS,
                 "Fact0 API and migration binary", ROOT / "api/THIRD_PARTY_LICENSES.txt")
    if args.component in ("all", "collector"):
        generate(ROOT / "claude-code-plugin/collector", (".",), COLLECTOR_TARGETS,
                 "Fact0 Claude Code collector", ROOT / "claude-code-plugin/THIRD_PARTY_LICENSES.txt")


if __name__ == "__main__":
    main()
