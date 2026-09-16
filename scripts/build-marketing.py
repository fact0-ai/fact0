#!/usr/bin/env python3
"""Build the public Fact0 website from an explicit source allowlist; never deploy it."""
import argparse
from html.parser import HTMLParser
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[1]
WEB = ROOT / "web"
MARKER = ".fact0-marketing-export.json"
GENERATOR = "fact0-static-marketing-v1"

# Keep this list explicit: new application routes do not enter the public build.
SOURCE_FILES = (
    "package.json", "package-lock.json", "tsconfig.json", "postcss.config.mjs", "content.json",
    "app/layout.tsx", "app/globals.css", "app/favicon.ico", "app/not-found.tsx",
    "app/robots.ts", "app/sitemap.ts",
    "app/(marketing)/layout.tsx", "app/(marketing)/page.tsx", "app/(marketing)/marketing.css",
    "app/(marketing)/legal/page.tsx", "app/(marketing)/legal/cookies/page.tsx",
    "app/(marketing)/legal/privacy/page.tsx", "app/(marketing)/legal/refund/page.tsx",
    "app/(marketing)/legal/terms/page.tsx",
    "components/blocks/navbar.tsx", "components/blocks/footer.tsx",
    "components/blocks/marketing-content.ts", "components/blocks/hero.tsx",
    "components/blocks/code-panel.tsx", "components/blocks/how-it-works.tsx",
    "components/blocks/features.tsx", "components/blocks/faq.tsx",
    "components/blocks/agent-flow-viz.tsx",
    "components/brand/brand-logo.tsx", "components/theme-provider.tsx",
    "lib/app-origin.ts", "lib/brand-assets.ts", "lib/docs-origin.ts", "lib/utils.ts",
    "fonts/dm-sans/DMSans-Regular.ttf", "fonts/dm-sans/DMSans-Italic.ttf",
    "fonts/dm-sans/DMSans-Medium.ttf", "fonts/dm-sans/DMSans-MediumItalic.ttf",
    "fonts/dm-sans/DMSans-SemiBold.ttf", "fonts/dm-sans/DMSans-SemiBoldItalic.ttf",
    "fonts/dm-sans/DMSans-Bold.ttf", "fonts/dm-sans/DMSans-BoldItalic.ttf", "fonts/dm-sans/OFL.txt",
    "fonts/bricolage-grotesque/BricolageGrotesque-Variable.woff2", "fonts/bricolage-grotesque/OFL.txt",
    "fonts/jetbrains-mono/JetBrainsMono-Variable.woff2", "fonts/jetbrains-mono/OFL.txt",
    "public/logo.svg", "public/og-image.svg", "public/llms.txt", "public/llms-full.txt",
    "public/favicon/favicon.ico", "public/favicon/favicon.svg",
    "public/favicon/favicon-16x16.png", "public/favicon/favicon-32x32.png",
    "public/favicon/apple-touch-icon.png", "public/favicon/android-chrome-192x192.png",
    "public/favicon/android-chrome-512x512.png", "public/favicon/site.webmanifest",
)
PAGES = ("index.html", "legal/index.html", "legal/cookies/index.html", "legal/privacy/index.html",
         "legal/refund/index.html", "legal/terms/index.html", "404.html")
BROWSER_PACKAGES = (
    "react", "react-dom", "scheduler", "next", "@swc/helpers", "styled-jsx",
    "next-themes", "lucide-react", "sonner", "clsx", "tailwind-merge",
    "@xyflow/react", "tw-animate-css", "tailwindcss", "@tailwindcss/typography",
)


def browser_dependency_notices():
    sections = ["Fact0 static website — browser dependency licenses and notices\n",
                "Includes framework-vendored notices; some framework modules are not used by every page.\n"]
    for name in BROWSER_PACKAGES:
        package = WEB / "node_modules" / name
        metadata = json.loads((package / "package.json").read_text())
        notices = sorted(path for path in package.rglob("*") if path.is_file() and
                         re.search(r"(^|[._-])(licen[cs]es?|notice)([._-]|$)", path.name, re.I))
        if not notices:
            raise ValueError(f"browser dependency has no license notice: {name}")
        sections.append(f"\n{'=' * 72}\n{name} {metadata['version']}\n{'=' * 72}\n")
        for notice in notices:
            if notice.is_symlink():
                raise ValueError(f"license notice must not be symlinked: {name}/{notice.relative_to(package)}")
            sections.append(f"\n--- {name}/{notice.relative_to(package)} ---\n{notice.read_text()}\n")
    return "".join(sections)


def check_replaceable(output):
    if output.is_symlink():
        raise ValueError("output must not be a symlink")
    if output.exists():
        marker = output / MARKER
        if not output.is_dir() or not marker.is_file() or json.loads(marker.read_text()).get("generator") != GENERATOR:
            raise ValueError("output exists and is not a generated marketing export; choose another --output")


def stage_sources(destination):
    for name in SOURCE_FILES:
        source = WEB / name
        if source.is_symlink() or not source.is_file():
            raise ValueError(f"missing or symlinked marketing source: {name}")
        target = destination / name
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(source, target)
    # Metadata handlers must explicitly opt into static export in Next.js 16.
    for name in ("app/robots.ts", "app/sitemap.ts"):
        target = destination / name
        with target.open("a") as stream:
            stream.write('\nexport const dynamic = "force-static";\n')
    (destination / "next.config.mjs").write_text(
        'export default { output: "export", trailingSlash: true, images: { unoptimized: true } };\n'
    )
    licenses = destination / "public/licenses"
    licenses.mkdir()
    shutil.copyfile(WEB / "fonts/dm-sans/OFL.txt", licenses / "DM-Sans-OFL.txt")
    shutil.copyfile(WEB / "fonts/bricolage-grotesque/OFL.txt", licenses / "Bricolage-Grotesque-OFL.txt")
    shutil.copyfile(WEB / "fonts/jetbrains-mono/OFL.txt", licenses / "JetBrains-Mono-OFL.txt")
    shutil.copyfile(ROOT / "LICENSE", licenses / "Fact0-MIT.txt")
    (licenses / "browser-dependencies.txt").write_text(browser_dependency_notices())
    # Reuse installed, locked build dependencies without copying or installing them.
    (destination / "node_modules").symlink_to(WEB / "node_modules", target_is_directory=True)


class LocalLinks(HTMLParser):
    def __init__(self):
        super().__init__()
        self.paths = []
        self.canonical = None

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if tag == "link" and attrs.get("rel") == "canonical":
            self.canonical = attrs.get("href")
        for name in ("href", "src"):
            value = attrs.get(name, "")
            if value.startswith("/") and not value.startswith("//"):
                self.paths.append(urlsplit(value).path)


def check_export(output, origin):
    for name in (*PAGES, "robots.txt", "sitemap.xml", "logo.svg", "og-image.svg",
                 "favicon/site.webmanifest", "llms.txt", "llms-full.txt",
                 "licenses/DM-Sans-OFL.txt", "licenses/Bricolage-Grotesque-OFL.txt",
                 "licenses/JetBrains-Mono-OFL.txt", "licenses/Fact0-MIT.txt", "licenses/browser-dependencies.txt"):
        if not (output / name).is_file():
            raise ValueError(f"missing public artifact: {name}")
    forbidden_routes = ("api", "v1", "dashboard", "executions", "sign-in")
    for route in forbidden_routes:
        if (output / route).exists() or (output / f"{route}.html").exists():
            raise ValueError(f"application route entered static export: {route}")
    forbidden_code = ("better-auth", "DATABASE_URL", "BETTER_AUTH_SECRET", "AUTH_WEBHOOK_SECRET",
                      "FACT0_BACKEND_URL", "AWS_SECRET_ACCESS_KEY", "@copilotkit")
    for path in output.rglob("*"):
        if path.is_symlink() or path.name.startswith(".env"):
            raise ValueError("symlink or environment file entered static export")
        if path.is_file() and path.suffix in (".html", ".js", ".json", ".txt", ".xml"):
            text = path.read_text()
            if any(token in text for token in forbidden_code):
                raise ValueError(f"application configuration entered static export: {path.relative_to(output)}")
    for name in PAGES:
        html = (output / name).read_text()
        links = LocalLinks()
        links.feed(html)
        for route in links.paths:
            if route.strip("/").split("/", 1)[0] in forbidden_routes:
                raise ValueError(f"application link remains in {name}")
            target = output / route.lstrip("/")
            if not target.is_file() and not (target / "index.html").is_file():
                raise ValueError(f"broken local asset/link in {name}: {route}")
        if name != "404.html":
            expected = origin + ("/legal/" if name.startswith("legal/") else "/")
            if links.canonical != expected:
                raise ValueError(f"incorrect canonical URL in {name}: {links.canonical}")
    home = (output / "index.html").read_text()
    if "Open dashboard" in home or "Run locally" not in home:
        raise ValueError("public navigation was not selected")
    if origin not in (output / "sitemap.xml").read_text():
        raise ValueError("sitemap origin does not match public origin")
    if not any(path.suffix in (".ttf", ".woff", ".woff2") for path in (output / "_next/static").rglob("*")):
        raise ValueError("local font assets are missing")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=ROOT / ".release-local/launch/marketing")
    parser.add_argument("--origin", default="https://fact0.io", help="Canonical public site origin")
    parser.add_argument("--docs-url", default="", help="Optional matching published docs URL; defaults to GitHub guides")
    args = parser.parse_args()
    origin = args.origin.strip().rstrip("/")
    parsed = urlsplit(origin)
    if parsed.scheme not in ("http", "https") or not parsed.hostname or parsed.username or parsed.password or parsed.path or parsed.query or parsed.fragment:
        parser.error("--origin must be an HTTP(S) origin without credentials or a path")
    output = args.output.expanduser().absolute()
    try:
        check_replaceable(output)
    except (ValueError, OSError) as error:
        parser.error(str(error))
    if not (WEB / "node_modules/next/dist/bin/next").is_file():
        parser.error("install the locked build dependencies first: npm --prefix web ci")
    node = shutil.which("node")
    if not node:
        parser.error("Node.js is required (use Node 22 or newer)")
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix=".marketing-build-", dir=output.parent) as temporary:
        work = Path(temporary)
        source = work / "source"
        source.mkdir()
        stage_sources(source)
        # Do not inherit provider credentials, old hosted URLs, NODE_OPTIONS or
        # any NEXT_PUBLIC_* variables from a developer/cloud environment.
        env = {name: os.environ[name] for name in ("PATH", "HOME", "TMPDIR", "TEMP", "TMP", "SYSTEMROOT", "LANG") if name in os.environ}
        env.update({"NODE_ENV": "production", "CI": "1", "NEXT_TELEMETRY_DISABLED": "1",
                    "NEXT_PUBLIC_SITE_URL": origin, "NEXT_PUBLIC_DOCS_URL": args.docs_url.strip(),
                    "NEXT_PUBLIC_MARKETING_ONLY": "1"})
        subprocess.run([node, str(WEB / "node_modules/next/dist/bin/next"), "build", "--webpack"], cwd=source, env=env, check=True)
        artifact = source / "out"
        check_export(artifact, origin)
        (artifact / MARKER).write_text(json.dumps({"generator": GENERATOR, "origin": origin, "source_files": SOURCE_FILES}, indent=2) + "\n")
        check_replaceable(output)
        previous = work / "previous-output"
        if output.exists():
            output.rename(previous)
        try:
            artifact.rename(output)
        except OSError:
            if previous.exists():
                previous.rename(output)
            raise
    print(f"Static marketing export ready: {output}")
    print("Checked public routes, links, canonical URLs, local fonts, licenses and absence of application/configuration code.")


if __name__ == "__main__":
    main()
