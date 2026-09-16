#!/usr/bin/env python3
"""Verify a disposable local Compose instance without printing credentials."""
import base64
import json
import os
from pathlib import Path
import secrets
import subprocess
import sys
import uuid
import zipfile

import requests
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey


ROOT = Path(__file__).resolve().parents[1]
LOCAL = ROOT / ".release-local"


def private_json(path, value):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w") as f:
        json.dump(value, f)


def main():
    LOCAL.mkdir(exist_ok=True)
    values = dict(line.split("=", 1) for line in (ROOT / ".env").read_text().splitlines() if line and not line.startswith("#"))
    origin = values["BETTER_AUTH_URL"]
    api = f"http://127.0.0.1:{values.get('FACT0_API_PORT', '8000')}"
    credentials_file = LOCAL / "smoke-owner.json"
    if credentials_file.exists():
        owner = json.loads(credentials_file.read_text())
    else:
        owner = {"email": "owner@example.invalid", "password": secrets.token_urlsafe(24)}
        private_json(credentials_file, owner)
    created = subprocess.run(["docker", "compose", "exec", "-T", "web", "node", "scripts/owner.mjs", "create", "--email", owner["email"], "--name", "Local test owner", "--password-stdin"], cwd=ROOT, input=owner["password"]+"\n", text=True, capture_output=True)
    if created.returncode:
        # CLI output may contain a generated key; keep it private on failure too.
        private_json(LOCAL / "owner-setup-error.json", {"stdout": created.stdout, "stderr": created.stderr})
        raise RuntimeError("Owner setup failed; inspect private .release-local/owner-setup-error.json")
    client = requests.Session()
    signed_in = client.post(origin + "/api/auth/sign-in/email", json=owner, headers={"Origin": origin}, timeout=15)
    if signed_in.status_code != 200:
        raise RuntimeError(f"Owner login failed: HTTP {signed_in.status_code}")
    token_response = client.get(origin + "/api/auth/token", timeout=15)
    token_response.raise_for_status()
    token = token_response.json()["token"]
    headers = {"Authorization": f"Bearer {token}"}
    response = client.post(origin + "/v1/me/keys", json={"scope": "write", "label": "compose-smoke"}, headers=headers, timeout=15)
    response.raise_for_status()
    key = response.json()["key"]
    private_json(LOCAL / "smoke-access.json", {"api_key": key, "token": token, "api": api, "origin": origin})
    print("Owner login, tenant provisioning, JWT and API-key creation passed.")

    denied = requests.post(origin + "/api/auth/sign-up/email", json={"email": "second@example.invalid", "password": secrets.token_urlsafe(20), "name": "Not allowed"}, headers={"Origin": origin}, timeout=15)
    assert denied.status_code in (403, 404), denied.status_code
    unauthorized = requests.get(api + "/api/v1/executions", timeout=15)
    assert unauthorized.status_code == 401, unauthorized.status_code
    unauthorized_write = requests.post(api + "/api/v1/executions", json={"agent_id": "anonymous"}, timeout=15)
    assert unauthorized_write.status_code == 401, unauthorized_write.status_code
    for method, path in (("POST", "/api/chat"), ("POST", "/api/email"),
                         ("GET", "/dashboard/settings/billing"), ("GET", "/dashboard/coding-agents/governance"),
                         ("POST", "/api/auth/organization/create"), ("POST", "/api/auth/organization/invite-member"),
                         ("POST", "/api/auth/organization/set-active")):
        rejected = client.request(method, origin + path, json={}, headers={"Origin": origin}, allow_redirects=False, timeout=15)
        assert rejected.status_code in (403, 404), (method, path, rejected.status_code)
    for method, path in (("POST", "/v1/me/share-links"), ("DELETE", "/v1/me/share-links/probe"),
                         ("GET", "/v1/public/share-links/probe/meta"), ("GET", "/v1/share/events"),
                         ("POST", "/v1/chain/reanchor"), ("POST", "/v1/chain/reanchor-all"),
                         ("POST", "/v1/me/billing/checkout"), ("POST", "/v1/me/billing/portal"),
                         ("POST", "/v1/me/billing/cancel"), ("GET", "/v1/me/billing/transactions"),
                         ("POST", "/webhooks/auth"), ("POST", "/api/webhooks/razorpay"),
                         ("POST", "/v1/otlp/v1/traces"), ("GET", "/api/v1/integrations/claude-code/policy"),
                         ("GET", "/v1/me/governance/policy"), ("PUT", "/v1/me/governance/policy"),
                         ("PUT", "/v1/me/settings/alerts"), ("POST", "/v1/me/settings/alerts/test"),
                         ("POST", "/v1/me/copilot/sessions"), ("POST", "/v1/me/copilot/quota/increment"),
                         ("GET", "/v1/admin/tenants"), ("POST", "/v1/admin/impersonate/start"),
                         ("GET", "/v1/receipts/probe")):
        rejected = client.request(method, api + path, json={}, headers=headers, timeout=15)
        assert rejected.status_code == 404, (method, path, rejected.status_code)

    env = dict(os.environ, FACT0_API_KEY=key, FACT0_BASE_URL=api, FACT0_EXAMPLE_OUTPUT=str(LOCAL / "python-evidence.zip"))
    result = subprocess.run([sys.executable, "examples/local-python.py"], cwd=ROOT, env=env, text=True, capture_output=True)
    if result.returncode:
        print(result.stderr, file=sys.stderr)
        raise RuntimeError("Python example failed")
    example = json.loads(result.stdout)
    print(f"Python persisted {example['spans']} spans; chain verification passed.")
    public_key = base64.b64encode(base64.b64decode(values["FACT0_SIGNING_KEY"])[32:]).decode()
    subprocess.run([sys.executable, "scripts/verify-export.py", str(LOCAL / "python-evidence.zip"), "--public-key", public_key], cwd=ROOT, check=True)
    with zipfile.ZipFile(LOCAL / "python-evidence.zip") as archive:
        manifest = json.loads(archive.read("manifest.sig"))
        try:
            Ed25519PublicKey.from_public_bytes(base64.b64decode(public_key)).verify(base64.b64decode(manifest["audit-report.pdf"]), archive.read("audit-report.pdf") + b"modified")
        except Exception:
            pass
        else:
            raise AssertionError("Modified export unexpectedly verified")

    # Reuse the exact idempotency key; timestamps are generated by the API.
    auth = {"Authorization": f"Bearer {key}"}
    body = {"agent_id": "smoke-idempotency", "idempotency_key": str(uuid.uuid4())}
    one = requests.post(api + "/api/v1/executions", json=body, headers=auth, timeout=15)
    two = requests.post(api + "/api/v1/executions", json=body, headers=auth, timeout=15)
    one.raise_for_status(); two.raise_for_status()
    assert one.json()["id"] == two.json()["id"], "Execution retry created a new ID"
    print("Duplicate execution creation retained the original ID; modified export was rejected.")
    requests.put(api + f"/api/v1/executions/{one.json()['id']}/end", json={"status": "COMPLETED"}, headers=auth, timeout=15).raise_for_status()
    state = {"execution_id": example["execution_id"], "public_key": public_key}
    private_json(LOCAL / "smoke-result.json", state)
    integration_env = dict(os.environ, FACT0_INTEGRATION_URL=api, FACT0_INTEGRATION_API_KEY=key)
    subprocess.run(["go", "test", "-count=1", "-run", "^TestCollectorLiveAPI$", "-v", "./..."], cwd=ROOT / "claude-code-plugin/collector", env=integration_env, check=True)
    print("Compose smoke checks passed. Credentials remain in ignored owner-readable files.")


if __name__ == "__main__":
    main()
