#!/usr/bin/env python3
"""Verify the delivered bytes, then exercise native startup in an isolated home."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import signal
import socket
import subprocess
import tarfile
import tempfile
import time
import urllib.error
import urllib.request
import zipfile


def check(value, message):
    if not value:
        raise AssertionError(message)


def sha(data):
    return hashlib.sha256(data).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('manifest', type=Path)
    args = parser.parse_args()
    manifest = json.loads(args.manifest.read_text(encoding='utf-8-sig'))
    host_os = {'Darwin': 'darwin', 'Linux': 'linux', 'Windows': 'windows'}[platform.system()]
    host_arch = {'x86_64': 'amd64', 'AMD64': 'amd64', 'arm64': 'arm64', 'aarch64': 'arm64'}[platform.machine()]
    check((manifest['os'], manifest['architecture']) == (host_os, host_arch), 'native runner architecture mismatch')
    package = manifest['package']
    check(Path(package['name']).name == package['name'], 'invalid package name')
    archive = args.manifest.parent / package['name']
    check(archive.stat().st_size == package['size'] and sha(archive.read_bytes()) == package['sha256'], 'package hash mismatch')
    pins = {entry['name']: entry for entry in manifest['files']}
    expected = {'wefto.exe', 'README-windows.md', 'install-windows.ps1', 'uninstall-windows.ps1'} if host_os == 'windows' else {'wefto', 'start.sh', 'README.md', 'LICENSE'}
    if host_os == 'darwin':
        expected.add('start.command')
    check(set(pins) == expected and len(pins) == len(manifest['files']), 'unexpected manifest members')
    with tempfile.TemporaryDirectory(prefix='wefto-native-') as tmp:
        root = Path(tmp).resolve()
        folder = root / '解压 package with spaces'
        folder.mkdir()
        members = {}
        if host_os == 'windows':
            with zipfile.ZipFile(archive) as source:
                check(len(source.infolist()) == len(expected), 'duplicate or extra ZIP members')
                members = {item.filename: (source.read(item), 0o644) for item in source.infolist()}
        else:
            with tarfile.open(archive) as source:
                for item in source:
                    check(item.isfile() and item.name == f'{archive.name[:-7]}/{Path(item.name).name}', 'unsafe tar member')
                    name = Path(item.name).name
                    check(name not in members, 'duplicate tar member')
                    members[name] = (source.extractfile(item).read(), item.mode)
        check(set(members) == expected, 'archive member mismatch')
        for name, (data, mode) in members.items():
            check(len(data) == pins[name]['size'] and sha(data) == pins[name]['sha256'], f'file hash mismatch: {name}')
            path = folder / name
            path.write_bytes(data)
            path.chmod(mode)
            if host_os != 'windows' and name in {'wefto', 'start.sh', 'start.command'}:
                check(mode == 0o755, f'missing executable permissions: {name}')
        home = root / 'home'
        home.mkdir()
        # Deliberately omit inherited host/provider credentials and agent overrides.
        env = {key: value for key, value in os.environ.items() if key.upper() in
               {'PATH', 'SYSTEMROOT', 'WINDIR', 'COMSPEC', 'PATHEXT', 'TMP', 'TEMP', 'TMPDIR'}}
        env.update(HOME=str(home), USERPROFILE=str(home), APPDATA=str(home / 'roaming'),
                   LOCALAPPDATA=str(home / 'local'), XDG_CONFIG_HOME=str(home / 'config'),
                   XDG_DATA_HOME=str(home / 'data'), XDG_CACHE_HOME=str(home / 'cache'),
                   XDG_STATE_HOME=str(home / 'state'))
        binary = folder / ('wefto.exe' if host_os == 'windows' else 'wefto')
        def run(*args):
            return subprocess.check_output([str(binary), *args], env=env, cwd=root, text=True, timeout=30)
        check(run('version').strip() == manifest['version'], 'wrong binary version')
        layout = json.loads(run('dirs'))
        for key, value in layout.items():
            if key != 'install':
                check(Path(value).resolve().is_relative_to(home), f'escaped isolated home: {key}')
        run('config', 'init')
        config_path = Path(layout['config']) / 'config.json'
        config = json.loads(config_path.read_text())
        config['portableAcceptance'] = {'keep': 'settings survive startup'}
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        for occupied in (False, True):
            with socket.socket() as holder:
                holder.bind(('127.0.0.1', 0))
                port = holder.getsockname()[1]
                if occupied:
                    holder.listen()
                else:
                    holder.close()
                config['listen']['address'] = f'127.0.0.1:{port}'
                config_path.write_text(json.dumps(config), encoding='utf-8')
                log_path = root / 'startup.log'
                command = [str(binary), 'serve'] if host_os == 'windows' else [str(folder / ('start.command' if host_os == 'darwin' else 'start.sh'))]
                with log_path.open('wb') as log:
                    process = subprocess.Popen(command, env=env, cwd=root, stdout=log, stderr=log)
                    try:
                        deadline = time.monotonic() + 60
                        url = None
                        while time.monotonic() < deadline:
                            check(process.poll() is None, 'startup exited: ' + log_path.read_text(errors='replace'))
                            match = re.search(r'Management: (http://127\.0\.0\.1:\d+/)', log_path.read_text(errors='replace'))
                            if match:
                                url = match[1]
                                break
                            time.sleep(0.1)
                        check(url, 'startup URL missing')
                        def request(path, token=None, method='GET'):
                            headers = {'Origin': url.rstrip('/')}
                            if token:
                                headers['Authorization'] = 'Bearer ' + token
                            return opener.open(urllib.request.Request(url + path, headers=headers, method=method), timeout=10)
                        health = json.load(request('healthz'))
                        check(health['service'] == 'wefto' and health['pid'] == process.pid, 'service identity mismatch')
                        check((url != f'http://127.0.0.1:{port}/') == occupied, 'port selection mismatch')
                        if occupied:
                            check(holder.fileno() >= 0 and holder.getsockname()[1] == port, 'occupied listener was disturbed')
                        token_path = Path(layout['state']) / 'router.lock' / 'management-token'
                        token = token_path.read_text().strip()
                        try:
                            request('api/service')
                            raise AssertionError('unauthenticated API accepted')
                        except urllib.error.HTTPError as error:
                            check(error.code == 401, 'wrong authentication rejection')
                        info = json.load(request('api/service', token))
                        check(info['version'] == manifest['version'] and info['pid'] == process.pid, 'authenticated service mismatch')
                        for asset in ('', 'app.js', 'router.css'):
                            check(len(request(asset).read()) > 100, 'embedded asset missing')
                        if host_os == 'windows':
                            check(request('api/service/stop', token, 'POST').status == 202, 'stop rejected')
                        else:
                            process.send_signal(signal.SIGINT if not occupied else signal.SIGTERM)
                        check(process.wait(timeout=30) == 0, 'unclean shutdown')
                        check(not token_path.exists(), 'service lock left behind')
                        config = json.loads(config_path.read_text())
                        check(config['portableAcceptance']['keep'] == 'settings survive startup', 'settings were lost')
                    finally:
                        if process.poll() is None:
                            process.kill()
                            process.wait(timeout=10)
    print(json.dumps(dict(result='PASS', os=host_os, architecture=host_arch, version=manifest['version'],
                         checks=['archive hashes and permissions', 'native launcher with Unicode/spaces',
                                 'isolated configuration', 'embedded UI', 'management authentication',
                                 'occupied-port fallback', 'SIGINT/SIGTERM or API stop', 'settings preserved'])))


if __name__ == '__main__':
    main()
