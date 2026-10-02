#!/usr/bin/env python3
"""Authenticated idle lease. Never print credentials; fail closed on old servers."""
import json
import pathlib
import sys
import urllib.error
import urllib.request


def api_key(path):
    values = {}
    for line in pathlib.Path(path).read_text().splitlines():
        key, sep, value = line.strip().partition('=')
        if sep and key in ('EA_API_KEY', 'EA_SERVER_API_KEY'):
            values[key] = value.strip().strip('\"\'')
    key = values.get('EA_SERVER_API_KEY') or values.get('EA_API_KEY')
    if key:
        return key
    raise ValueError('server API key not configured')


def main():
    action, url, env_file, lease_file = sys.argv[1:]
    key = api_key(env_file)
    if not key:
        raise ValueError('empty API key')
    headers = {'Authorization': 'Bearer ' + key, 'Content-Type': 'application/json'}
    path = pathlib.Path(lease_file)
    method = 'POST' if action == 'prepare' else 'DELETE'
    body = {} if action == 'prepare' else {'lease': path.read_text().strip()}
    request = urllib.request.Request(url, data=json.dumps(body).encode(), headers=headers, method=method)
    try:
        with urllib.request.urlopen(request, timeout=10) as response:
            result = json.load(response)
    except urllib.error.HTTPError as error:
        error.close()
        if error.code == 409 and action == 'prepare':
            print('Service busy; deployment deferred until a later update check.')
            return 75
        print('Deployment admission refused (HTTP %s); existing service preserved.' % error.code, file=sys.stderr)
        return 1
    if action == 'prepare':
        lease = result.get('lease')
        if not isinstance(lease, str) or len(lease) != 48 or result.get('active') != 0:
            raise ValueError('invalid deployment lease')
        path.write_text(lease)
        path.chmod(0o600)
    return 0


if __name__ == '__main__':
    try:
        sys.exit(main())
    except Exception as error:
        # Errors can carry URLs, headers or response bodies. Print only their class.
        print('Deployment guard failed (%s); refusing unsafe update.' % type(error).__name__, file=sys.stderr)
        sys.exit(1)
