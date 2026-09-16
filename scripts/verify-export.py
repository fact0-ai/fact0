#!/usr/bin/env python3
"""Verify a Fact0 export's file signatures. pip install cryptography"""
import argparse
import base64
import json
from pathlib import Path
import zipfile

from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey


def verify_archive(path, trusted_key):
    with zipfile.ZipFile(path) as archive:
        names = archive.namelist()
        if len(names) != len(set(names)):
            raise ValueError("Duplicate archive members make this export ambiguous; verification refused.")
        manifest = json.loads(archive.read("manifest.sig"))
        if manifest["public_key"] != trusted_key:
            raise ValueError("Export key does not match the trusted instance key.")
        key = Ed25519PublicKey.from_public_bytes(base64.b64decode(trusted_key, validate=True))
        for name in ("audit-report.pdf", "verification.json"):
            key.verify(base64.b64decode(manifest[name], validate=True), archive.read(name))
        return json.loads(archive.read("verification.json"))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("archive", type=Path)
    parser.add_argument("--public-key", required=True, help="Trusted base64 public key recorded from the instance separately")
    args = parser.parse_args()
    verification = verify_archive(args.archive, args.public_key)
    print("File signatures valid for the trusted instance key.")
    print(f"Recorded chain verification result: {verification.get('valid')}")
    print("This does not establish that every agent action was captured or that the signer is trustworthy.")
    if verification.get("valid") is not True:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
