#!/usr/bin/env python3
"""Create local instance secrets once. Requires Python 3 and OpenSSL >= 1.1.1."""
import argparse
import base64
import os
import ipaddress
from pathlib import Path
import re
import secrets
import socket
import subprocess
from urllib.parse import urlsplit


def normalize_origin(value):
    if any(c.isspace() or ord(c) < 32 or c in "\\$#" for c in value):
        raise ValueError("origin contains unsupported whitespace or characters")
    origin = urlsplit(value)
    if origin.scheme not in ("http", "https") or not origin.hostname or origin.username is not None or origin.password is not None or origin.path not in ("", "/") or origin.query or origin.fragment:
        raise ValueError("use an HTTP(S) origin without credentials or a path")
    host = origin.hostname.lower()
    if ":" in host:
        if "%" in host:
            raise ValueError("IPv6 zone identifiers are not supported in browser origins")
        host = f"[{ipaddress.IPv6Address(host).compressed}]"
    elif re.fullmatch(r"[0-9.]+", host):
        host = str(ipaddress.IPv4Address(host))
    elif not re.fullmatch(r"[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?\.?", host):
        raise ValueError("use an ASCII hostname (punycode for international names)")
    else:
        # Browsers treat a numeric final label as IPv4, including hexadecimal
        # and shortened forms. Reject those aliases instead of writing an
        # issuer that the browser will rewrite or refuse.
        final_label = host.rstrip(".").rsplit(".", 1)[-1]
        if re.fullmatch(r"(?:[0-9]+|0x[0-9a-f]*)", final_label):
            raise ValueError("use standard dotted-decimal IPv4 or a hostname without a numeric final label")
    port = origin.port  # urlsplit rejects malformed/out-of-range ports here.
    if port is not None and port < 1:
        raise ValueError("port must be between 1 and 65535")
    suffix = "" if port is None or (origin.scheme, port) in (("http", 80), ("https", 443)) else f":{port}"
    return f"{origin.scheme}://{host}{suffix}", port


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=Path(__file__).resolve().parents[1] / ".env")
    parser.add_argument("--origin", default="http://localhost:3000")
    parser.add_argument("--api-port", type=int, default=8000)
    parser.add_argument("--web-port", type=int, help="Loopback web port; defaults to the HTTP origin port (80 when omitted) or 3000 for a TLS proxy")
    args = parser.parse_args()
    try:
        origin, origin_port = normalize_origin(args.origin)
    except ValueError as error:
        parser.error(str(error))
    web_port = args.web_port
    if web_port is None:
        web_port = (origin_port or 80) if origin.startswith("http:") else 3000
    for name, port in (("web", web_port), ("api", args.api_port)):
        if not 1 <= port <= 65535:
            parser.error(f"--{name}-port must be between 1 and 65535")
    if web_port == args.api_port:
        parser.error("Web and API ports must differ")
    if args.output.exists():
        print(f"Keeping existing {args.output}; no secrets changed.")
        return
    for name, port in (("web", web_port), ("api", args.api_port)):
        try:
            with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as probe:
                probe.bind(("127.0.0.1", port))
        except OSError:
            parser.error(f"Local port {port} is in use; choose another --{name}-port (and matching --origin for direct web access).")
    private_der = subprocess.check_output(["openssl", "genpkey", "-algorithm", "ED25519", "-outform", "DER"])
    public_der = subprocess.check_output(["openssl", "pkey", "-inform", "DER", "-pubout", "-outform", "DER"], input=private_der)
    if len(private_der) != 48 or len(public_der) != 44:
        raise SystemExit("Unsupported OpenSSL Ed25519 encoding; environment was not written.")
    signing_key = base64.b64encode(private_der[-32:] + public_der[-32:]).decode()
    values = {
        "POSTGRES_PASSWORD": secrets.token_hex(24),
        "BETTER_AUTH_SECRET": secrets.token_hex(32),
        "AUTH_WEBHOOK_SECRET": secrets.token_hex(32),
        "FACT0_SIGNING_KEY": signing_key,
        "BETTER_AUTH_URL": origin,
        "FACT0_WEB_PORT": str(web_port),
        "FACT0_API_PORT": str(args.api_port),
    }
    fd = os.open(args.output, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w") as f:
        f.write("# Local instance secrets. Back up securely; never commit this file.\n")
        for key, value in values.items():
            f.write(f"{key}={value}\n")
    print(f"Created {args.output} with owner-only permissions. Secrets are not printed.")


if __name__ == "__main__":
    main()
