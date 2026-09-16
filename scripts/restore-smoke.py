#!/usr/bin/env python3
"""Restore the disposable Compose test instance into a second, isolated volume."""
import json
import os
from pathlib import Path
import secrets
import subprocess
import uuid

import requests

ROOT = Path(__file__).resolve().parents[1]
LOCAL = ROOT / '.release-local'


def main():
    access = json.loads((LOCAL / 'smoke-access.json').read_text())
    owner = json.loads((LOCAL / 'smoke-owner.json').read_text())
    known = json.loads((LOCAL / 'smoke-result.json').read_text())['execution_id']
    auth = {'Authorization': 'Bearer ' + access['api_key']}
    original = requests.get(access['api'] + '/api/v1/executions/' + known, headers=auth, timeout=15)
    original.raise_for_status()
    project = 'fact0-restore-' + uuid.uuid4().hex[:8]
    dump = LOCAL / (project + '.dump')
    override = LOCAL / (project + '.json')
    override.write_text(json.dumps({'services': {service: {'image': 'fact0-' + service} for service in ['api', 'web', 'migrate']}}))
    env = dict(os.environ, FACT0_WEB_PORT='3100', FACT0_API_PORT='18100', BETTER_AUTH_URL='http://localhost:3100')
    command = ['docker', 'compose', '-p', project, '-f', str(ROOT / 'compose.yaml'), '-f', str(override)]
    def compose(*args, **kwargs):
        return subprocess.run(command + list(args), cwd=ROOT, env=env, check=True, **kwargs)
    subprocess.run(['bash', 'scripts/backup.sh', str(dump)], cwd=ROOT, check=True)
    try:
        compose('up', '-d', '--wait', 'postgres')
        with dump.open('rb') as source:
            compose('exec', '-T', 'postgres', 'pg_restore', '-U', 'fact0', '-d', 'fact0', '--exit-on-error', stdin=source)
        compose('up', '-d', '--wait', '--no-build')
        api = 'http://127.0.0.1:18100'
        origin = env['BETTER_AUTH_URL']
        restored = requests.get(api + '/api/v1/executions/' + known, headers=auth, timeout=15)
        restored.raise_for_status()
        assert restored.json() == original.json(), 'Restored execution differs'
        verified = requests.get(api + '/v1/verify', headers=auth, timeout=15)
        verified.raise_for_status()
        assert verified.json()['valid'] is True
        session = requests.Session()
        login = session.post(origin + '/api/auth/sign-in/email', json=owner, headers={'Origin':origin}, timeout=15)
        login.raise_for_status()
        cookies = list(session.cookies)
        assert cookies and all(not c.domain_specified for c in cookies), 'Auth cookies must be host-only'
        assert session.get(origin + '/api/auth/get-session', timeout=15).json()['user']['email'] == owner['email']
        # The new public origin must issue an owner token accepted by its private API proxy.
        token = session.get(origin + '/api/auth/token', timeout=15)
        token.raise_for_status()
        keys = session.get(origin + '/v1/me/keys', headers={'Authorization':'Bearer ' + token.json()['token']}, timeout=15)
        keys.raise_for_status()
        compose('restart', 'postgres', 'web', 'api')
        compose('up', '-d', '--wait', '--no-build')
        after_restart = requests.get(api + '/api/v1/executions/' + known, headers=auth, timeout=15)
        after_restart.raise_for_status()
        assert after_restart.json() == original.json()
        # Exercise operator password recovery only in the isolated restored copy.
        replacement = secrets.token_urlsafe(24)
        compose('exec', '-T', 'web', 'node', 'scripts/owner.mjs', 'reset-password', '--email', owner['email'], '--password-stdin', input=replacement+'\n', text=True, capture_output=True)
        assert session.get(origin + '/api/auth/get-session', timeout=15).json() is None, 'Reset did not revoke old session'
        old_login = requests.post(origin + '/api/auth/sign-in/email', json=owner, headers={'Origin':origin}, timeout=15)
        assert old_login.status_code == 401
        new_login = requests.post(origin + '/api/auth/sign-in/email', json={**owner,'password':replacement}, headers={'Origin':origin}, timeout=15)
        new_login.raise_for_status()
        (LOCAL / 'restore-result.json').write_text(json.dumps({'backup_restore':True,'restart_persistence':True,'owner_reset':True,'host_only_cookies':True,'changed_origin':True,'known_execution_id':known},indent=2))
        print('Backup/restore, restart persistence, changed origin, host-only cookies and owner password reset passed.')
    finally:
        compose('down', '--volumes', '--remove-orphans')
        override.unlink(missing_ok=True)


if __name__ == '__main__':
    main()
