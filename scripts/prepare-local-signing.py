#!/usr/bin/env python3
"""Create a persistent, project-owned identity. Does not grant certificate trust.
Use --trust-for-codesign only after explicitly approving the user-level trust change.
No Apple certificate, global keychain defaults, Gatekeeper or TLS trust is changed.
"""
import argparse
import hashlib
import json
import os
import secrets
import shlex
import subprocess
import sys
from pathlib import Path

ROOT = Path.home() / 'Library/Application Support/EasyAgent Signing'

def command(args):
    result = subprocess.run(args, capture_output=True, text=True)
    if result.returncode:
        # Commands can contain keychain passwords; never include argv or raw output.
        raise RuntimeError('Signing setup step failed: ' + args[0] + ' ' + args[1])
    return result.stdout

def prepare(root=ROOT):
    root.mkdir(parents=True, exist_ok=True, mode=0o700)
    if root.is_symlink() or root.stat().st_uid != os.getuid() or root.stat().st_mode & 0o077:
        raise RuntimeError('Signing directory must be private to the current user')
    config = root / 'identity.json'
    if config.exists():
        value = json.loads(config.read_text())
        if not (root / 'signing.keychain-db').exists() or not (root / 'password').exists():
            raise RuntimeError('Existing identity is incomplete; refusing to regenerate it')
        der = subprocess.check_output(['openssl', 'x509', '-in', str(root / 'certificate.pem'), '-outform', 'DER'])
        if value.get('identity') != hashlib.sha1(der).hexdigest().upper() or value.get('certificateSHA256') != hashlib.sha256(der).hexdigest():
            raise RuntimeError('Existing certificate identity changed; refusing trust or regeneration')
        if value.get('keychain') != str(root / 'signing.keychain-db') or value.get('passwordFile') != str(root / 'password'):
            raise RuntimeError('Existing identity points outside its project signing directory')
        return value
    if any(root.iterdir()):
        raise RuntimeError('Incomplete setup exists; preserve it and inspect before retrying')
    if not command(['openssl', 'version']).startswith('OpenSSL 3.'):
        raise RuntimeError('Initial identity setup requires OpenSSL 3 (for example Homebrew openssl@3)')
    password = secrets.token_urlsafe(48)
    password_file = root / 'password'
    password_file.write_text(password)
    os.chmod(password_file, 0o600)
    key, cert, archive = root / 'key.pem', root / 'certificate.pem', root / 'identity.p12'
    keychain = root / 'signing.keychain-db'
    original = shlex.split(command(['security', 'list-keychains', '-d', 'user']))
    try:
        command(['openssl', 'req', '-x509', '-newkey', 'rsa:3072', '-nodes', '-keyout', str(key), '-out', str(cert), '-days', '3650', '-subj', '/CN=EasyAgent Local Signing', '-addext', 'basicConstraints=critical,CA:FALSE', '-addext', 'keyUsage=critical,digitalSignature', '-addext', 'extendedKeyUsage=codeSigning'])
        os.chmod(key, 0o600)
        command(['openssl', 'pkcs12', '-export', '-legacy', '-inkey', str(key), '-in', str(cert), '-out', str(archive), '-passout', 'file:' + str(password_file)])
        os.chmod(archive, 0o600)
        command(['security', 'create-keychain', '-p', password, str(keychain)])
        command(['security', 'unlock-keychain', '-p', password, str(keychain)])
        command(['security', 'set-keychain-settings', '-lut', '21600', str(keychain)])
        command(['security', 'import', str(archive), '-k', str(keychain), '-P', password, '-T', '/usr/bin/codesign'])
        command(['security', 'set-key-partition-list', '-S', 'apple-tool:,apple:,codesign:', '-s', '-k', password, str(keychain)])
        os.chmod(keychain, 0o600)
        der = subprocess.check_output(['openssl', 'x509', '-in', str(cert), '-outform', 'DER'])
        value = {'version': 1, 'identity': hashlib.sha1(der).hexdigest().upper(), 'certificateSHA256': hashlib.sha256(der).hexdigest(), 'keychain': str(keychain), 'passwordFile': str(password_file)}
        config.write_text(json.dumps(value, indent=2) + '\n')
        os.chmod(config, 0o600)
        return value
    finally:
        try:
            current = shlex.split(command(['security', 'list-keychains', '-d', 'user']))
            restored = current if str(keychain) in original else [item for item in current if item != str(keychain)]
            command(['security', 'list-keychains', '-d', 'user', '-s', *restored])
        finally:
            for file in [key, archive]:
                if file.exists():
                    file.unlink()

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--trust-for-codesign', action='store_true', help='Explicitly approve trust for this project certificate and /usr/bin/codesign only')
    args = parser.parse_args()
    if sys.platform != 'darwin':
        parser.error('Local code-signing setup requires macOS')
    value = prepare()
    if args.trust_for_codesign:
        command(['security', 'add-trusted-cert', '-r', 'trustRoot', '-p', 'codeSign', '-a', '/usr/bin/codesign', '-k', value['keychain'], str(ROOT / 'certificate.pem')])
    print(json.dumps({'config': str(ROOT / 'identity.json'), 'certificateSHA256': value['certificateSHA256'], 'trustUpdated': args.trust_for_codesign}))

if __name__ == '__main__':
    main()
