#!/usr/bin/env python3
"""Build a fixed, self-contained Unix distribution (Python is build-time only)."""
import argparse
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile


def sha(data):
    return hashlib.sha256(data).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--version', required=True)
    parser.add_argument('--os', required=True, choices=['darwin', 'linux'])
    parser.add_argument('--arch', required=True, choices=['amd64', 'arm64'])
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--go', default='go')
    args = parser.parse_args()
    if not re.fullmatch(r'\d+\.\d+\.\d+(?:-[A-Za-z0-9.-]+)?(?:\+[A-Za-z0-9.-]+)?', args.version):
        parser.error('an explicit semantic version is required')
    if not args.output.is_absolute():
        parser.error('--output must be absolute')
    args.output.mkdir(parents=True, exist_ok=True)
    base = f'wefto-{args.version}-{args.os}-{args.arch}'
    archive = args.output / (base + '.tar.gz')
    manifest = args.output / (base + '.manifest.json')
    sums = args.output / 'SHA256SUMS'
    for target in (archive, manifest, sums):
        if target.exists():
            raise SystemExit(f'refusing to overwrite {target}')
    module = Path(__file__).resolve().parent.parent
    env = dict(os.environ, GOOS=args.os, GOARCH=args.arch, CGO_ENABLED='0',
               GOENV='off', GOTOOLCHAIN='local', GOWORK='off', GOPROXY='off', GOSUMDB='off')
    with tempfile.TemporaryDirectory(prefix='wefto-package-') as stage:
        binary = Path(stage) / 'wefto'
        subprocess.run([args.go, 'build', '-trimpath', '-buildvcs=false', '-ldflags',
                        f'-X main.version={args.version} -buildid=', '-o', str(binary),
                        './cmd/wefto'], cwd=module, env=env, check=True)
        launcher = (module / 'scripts/start-unix.sh').read_bytes().replace(b'\r\n', b'\n')
        files = {'wefto': (binary.read_bytes(), 0o755), 'start.sh': (launcher, 0o755),
                 'README.md': ((module / 'docs/unix-delivery.md').read_bytes().replace(b'\r\n', b'\n'), 0o644),
                 'LICENSE': ((module / 'LICENSE').read_bytes().replace(b'\r\n', b'\n'), 0o644)}
        if args.os == 'darwin':
            files['start.command'] = (launcher, 0o755)
        with archive.open('xb') as raw:
            with gzip.GzipFile(filename='', mode='wb', fileobj=raw, mtime=0) as compressed:
                with tarfile.open(fileobj=compressed, mode='w', format=tarfile.USTAR_FORMAT) as tar:
                    for name, (data, mode) in sorted(files.items()):
                        member = tarfile.TarInfo(f'{base}/{name}')
                        member.size, member.mode, member.mtime = len(data), mode, 946684800
                        tar.addfile(member, io.BytesIO(data))
        record = dict(schemaVersion=1, product='wefto', version=args.version,
                      os=args.os, architecture=args.arch,
                      package=dict(name=archive.name, size=archive.stat().st_size, sha256=sha(archive.read_bytes())),
                      files=[dict(name=name, size=len(data), sha256=sha(data), mode=oct(mode))
                             for name, (data, mode) in sorted(files.items())])
        with manifest.open('x', encoding='utf-8', newline='\n') as output:
            output.write(json.dumps(record, ensure_ascii=False, indent=2) + '\n')
        with sums.open('x', encoding='utf-8', newline='\n') as output:
            for path in (archive, manifest):
                output.write(f'{sha(path.read_bytes())}  {path.name}\n')
    print(archive)


if __name__ == '__main__':
    main()
