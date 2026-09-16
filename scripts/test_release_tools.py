"""Regression checks for release tooling; uses synthetic keys and temporary files."""
import base64
from contextlib import redirect_stderr, redirect_stdout
import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch
import warnings
import zipfile

from cryptography.exceptions import InvalidSignature
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat

ROOT = Path(__file__).resolve().parents[1]


def module(name):
    spec = importlib.util.spec_from_file_location(name, ROOT / "scripts" / f"{name}.py")
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


class ReleaseToolsTest(unittest.TestCase):
    def test_origins_match_browser_normalization(self):
        normalize = module("init-env").normalize_origin
        for value, expected in (
            ("HTTP://LOCALHOST:80", "http://localhost"),
            ("https://Example.COM:443/", "https://example.com"),
            ("http://[0:0:0:0:0:0:0:1]:3000", "http://[::1]:3000"),
            ("http://127.0.0.1:3000", "http://127.0.0.1:3000"),
            ("http://fact0.example.:3000", "http://fact0.example.:3000"),
            ("http://123.example:3000", "http://123.example:3000"),
        ):
            with self.subTest(value=value):
                self.assertEqual(normalize(value)[0], expected)
        for invalid in (
            "http://local\\host", "http://localhost:99999", "http://localhost:0",
            "http://user@localhost", "http://localhost/path", "http://localhost bad",
            "http://127.1", "http://2130706433", "http://0177.0.0.1", "http://127.0.0.01",
            "http://0x7f000001:3000", "http://0X7F000001.:3000", "http://0x7f.0.0.1:3000",
            "http://example.123:3000", "http://example.123.:3000", "http://example.0xabc:3000",
            "http://example.0x:3000", "http://[fe80::1%eth0]:3000",
        ):
            with self.subTest(value=invalid), self.assertRaises(ValueError):
                normalize(invalid)

    def test_init_env_writes_effective_http_and_proxy_ports(self):
        setup = module("init-env")
        cases = (
            ([], "http://localhost:3000", "3000"),
            (["--origin", "http://localhost"], "http://localhost", "80"),
            (["--origin", "HTTP://LOCALHOST:80/"], "http://localhost", "80"),
            (["--origin", "http://localhost:3100"], "http://localhost:3100", "3100"),
            (["--origin", "https://example.com"], "https://example.com", "3000"),
            (["--origin", "https://example.com:8443"], "https://example.com:8443", "3000"),
            (["--origin", "https://example.com", "--web-port", "3100"], "https://example.com", "3100"),
            (["--origin", "http://localhost", "--web-port", "3100"], "http://localhost", "3100"),
        )
        for flags, origin, web_port in cases:
            with self.subTest(flags=flags), tempfile.TemporaryDirectory() as temporary:
                output = Path(temporary) / ".env"
                with patch("sys.argv", ["init-env.py", "--output", str(output), *flags]), \
                     patch.object(setup.socket, "socket") as socket_mock, \
                     patch.object(setup.subprocess, "check_output", side_effect=[b"p" * 48, b"q" * 44]), \
                     patch.object(setup.secrets, "token_hex", return_value="synthetic-secret"), \
                     redirect_stdout(io.StringIO()) as stdout:
                    setup.main()
                values = dict(line.split("=", 1) for line in output.read_text().splitlines() if not line.startswith("#"))
                self.assertEqual(values["BETTER_AUTH_URL"], origin)
                self.assertEqual(values["FACT0_WEB_PORT"], web_port)
                self.assertEqual(values["FACT0_API_PORT"], "8000")
                self.assertEqual(output.stat().st_mode & 0o777, 0o600)
                self.assertNotIn("synthetic-secret", stdout.getvalue())
                self.assertEqual(socket_mock.return_value.__enter__.return_value.bind.call_args_list[0].args, (("127.0.0.1", int(web_port)),))

    def test_init_env_refuses_invalid_explicit_web_ports_without_side_effects(self):
        setup = module("init-env")
        for port, existing in (("0", False), ("-1", False), ("65536", False), ("0", True)):
            with self.subTest(port=port, existing=existing), tempfile.TemporaryDirectory() as temporary:
                output = Path(temporary) / ".env"
                if existing:
                    output.write_text("keep existing secrets\n")
                with patch("sys.argv", ["init-env.py", "--output", str(output), "--web-port", port]), \
                     patch.object(setup.socket, "socket") as socket_mock, \
                     patch.object(setup.subprocess, "check_output") as openssl_mock, \
                     redirect_stderr(io.StringIO()) as stderr, self.assertRaises(SystemExit) as error:
                    setup.main()
                self.assertEqual(error.exception.code, 2)
                self.assertIn("--web-port must be between 1 and 65535", stderr.getvalue())
                socket_mock.assert_not_called()
                openssl_mock.assert_not_called()
                if existing:
                    self.assertEqual(output.read_text(), "keep existing secrets\n")
                else:
                    self.assertFalse(output.exists())

    def archive(self, duplicate=False, tamper=False):
        key = Ed25519PrivateKey.generate()
        public = base64.b64encode(key.public_key().public_bytes(Encoding.Raw, PublicFormat.Raw)).decode()
        contents = {"audit-report.pdf": b"synthetic PDF", "verification.json": b'{"valid":true}'}
        manifest = {"public_key": public, **{name: base64.b64encode(key.sign(body)).decode() for name, body in contents.items()}}
        out = io.BytesIO()
        with zipfile.ZipFile(out, "w") as archive:
            if duplicate:
                archive.writestr("audit-report.pdf", b"different first entry")
            for name, body in contents.items():
                with warnings.catch_warnings():
                    warnings.simplefilter("ignore", UserWarning)
                    archive.writestr(name, body + (b"modified" if tamper else b""))
            archive.writestr("manifest.sig", json.dumps(manifest))
        out.seek(0)
        return out, public

    def test_trusted_signature_and_tampering(self):
        verify = module("verify-export").verify_archive
        self.assertTrue(verify(*self.archive())["valid"])
        with self.assertRaises(InvalidSignature):
            verify(*self.archive(tamper=True))

    def test_duplicate_archive_names_refused(self):
        with self.assertRaisesRegex(ValueError, "Duplicate"):
            module("verify-export").verify_archive(*self.archive(duplicate=True))

    def test_backup_publication_is_atomic_and_does_not_follow_symlinks(self):
        with tempfile.TemporaryDirectory() as temporary:
            folder = Path(temporary)
            fake = folder / "docker"
            fake.write_text("#!/bin/sh\nsleep 0.05\nprintf 'synthetic backup'\n")
            fake.chmod(0o700)
            env = dict(os.environ, PATH=str(folder) + os.pathsep + os.environ["PATH"])
            output = folder / "backup.dump"
            cmd = ["bash", str(ROOT / "scripts/backup.sh"), str(output)]
            processes = [subprocess.Popen(cmd, env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE) for _ in range(2)]
            for process in processes:
                process.communicate(timeout=10)
            self.assertEqual(sorted(p.returncode for p in processes), [0, 1])
            self.assertEqual(output.read_bytes(), b"synthetic backup")
            protected = folder / "protected"
            protected.write_text("keep")
            link = folder / "symlink.dump"
            link.symlink_to(protected)
            result = subprocess.run(cmd[:-1] + [str(link)], env=env, capture_output=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(protected.read_text(), "keep")

    def test_scanner_refuses_dangling_symlinks_and_force_added_backups(self):
        for kind in ("symlink", "backup"):
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as temporary:
                folder = Path(temporary)
                (folder / "scripts").mkdir()
                shutil.copy2(ROOT / "scripts/scan-release.py", folder / "scripts/scan-release.py")
                subprocess.run(["git", "init", "-q", str(folder)], check=True)
                if kind == "symlink":
                    (folder / "dangling").symlink_to(folder / "missing")
                else:
                    (folder / ".gitignore").write_text("backups/\n")
                    (folder / "backups").mkdir()
                    (folder / "backups/database.dump").write_bytes(b"private database")
                    subprocess.run(["git", "-C", str(folder), "add", "-f", "backups/database.dump"], check=True)
                result = subprocess.run([os.sys.executable, str(folder / "scripts/scan-release.py"), "--gitleaks", "/usr/bin/true"], capture_output=True, text=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertTrue("symlink" in result.stderr or "Private/operator" in result.stderr, result.stderr)


if __name__ == "__main__":
    unittest.main()
