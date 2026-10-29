#!/usr/bin/env python3
"""Prepare/start the 端云协同 product on macOS/Linux; Python 3.9+, stdlib only."""
import argparse
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import platform
import re
import shutil
import struct
import subprocess
import sys
import tempfile
import tarfile
import time
import urllib.error
import urllib.parse
import urllib.request
import webbrowser

REPO = "https://github.com/XHToken/magpie.git"
API = "https://api.github.com/repos/XHToken/magpie"
TAG = re.compile(r"wefto-v\d+\.\d+\.\d+(?:-[A-Za-z0-9.-]+)?(?:\+[A-Za-z0-9.-]+)?\Z")


def plain(path):
    path = Path(path).expanduser()
    if not path.is_absolute() or ".." in path.parts:
        raise RuntimeError("Use an absolute path without '..'.")
    for item in (path, *path.parents):
        if item.is_symlink():
            if sys.platform == "darwin" and str(item) in ("/var", "/tmp", "/etc") and item.is_dir():
                continue
            raise RuntimeError(f"Refusing symlink path: {item}")
    return path


def api(suffix):
    request = urllib.request.Request(API + suffix, headers={
        "User-Agent": "api-router-start-skill", "Accept": "application/vnd.github+json"})
    with urllib.request.urlopen(request, timeout=30) as response:
        return json.load(response)


def run(args, **kwargs):
    kwargs.setdefault("timeout", 900)
    return subprocess.check_output([str(x) for x in args], text=True, **kwargs).strip()


def router_json(exe, *args):
    return json.loads(run([exe, *args]))


def platform_id():
    system = {"Darwin": "darwin", "Linux": "linux"}.get(platform.system())
    arch = {"x86_64": "amd64", "amd64": "amd64", "aarch64": "arm64", "arm64": "arm64"}.get(platform.machine().lower())
    if system == "darwin":
        version = platform.mac_ver()[0]
        if version and int(version.split('.')[0]) < 12:
            raise RuntimeError("The macOS packages require macOS 12 or newer.")
        # A translated Python process can report x86_64 on Apple Silicon.
        if arch == "amd64":
            try:
                if run(["/usr/sbin/sysctl", "-n", "hw.optional.arm64"]) == "1":
                    arch = "arm64"
            except (OSError, subprocess.SubprocessError):
                pass
    if not system or not arch:
        raise RuntimeError("Supported here: macOS/Linux amd64/arm64. Windows uses start-api-router.ps1.")
    return system, arch


def validate_target(target, system):
    home = Path.home()
    if system == "darwin":
        protected = [home / "Library/Application Support/wefto",
                     home / "Library/Caches/wefto", home / "Library/Logs/wefto"]
        for key in ("XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"):
            if os.environ.get(key):
                protected.append(Path(os.environ[key]) / "wefto")
    else:
        protected = [Path(os.environ.get(key) or str(home / fallback)) / "wefto" for key, fallback in (
            ("XDG_CONFIG_HOME", ".config"), ("XDG_DATA_HOME", ".local/share"),
            ("XDG_STATE_HOME", ".local/state"), ("XDG_CACHE_HOME", ".cache"))]
    protected.extend(root.with_name("api-router") for root in list(protected))
    for root in protected:
        root = plain(root)
        if root == target or root in target.parents or target in root.parents:
            raise RuntimeError("Program directory overlaps configuration/data/cache: " + str(root))


def discover(system, arch, tag, stable=False):
    if tag:
        if not TAG.fullmatch(tag):
            raise RuntimeError("Expected wefto-vVERSION from XHToken/magpie, not a legacy API-router or upstream Magpie tag.")
        release = api("/releases/tags/" + urllib.parse.quote(tag, safe=""))
        if release.get("draft") or (stable and release.get("prerelease")):
            raise RuntimeError("Release is a draft or excluded by --stable-only.")
        releases = [release]
        ref = tag
    else:
        releases = []
        for page in range(1, 21):
            batch = api(f"/releases?per_page=100&page={page}")
            releases.extend(r for r in batch if not r.get("draft") and TAG.fullmatch(r["tag_name"])
                            and not (stable and r.get("prerelease")))
            if len(batch) < 100:
                break
        else:
            raise RuntimeError("Release discovery exceeded 20 pages; specify --tag.")
        ref = "main"
    if stable and not releases:
        raise RuntimeError("No stable 端云协同 release. Omit --stable-only if previews are acceptable.")
    for release in sorted(releases, key=lambda r: r.get("published_at", ""), reverse=True):
        if not TAG.fullmatch(release["tag_name"]) or (tag and release["tag_name"] != tag):
            raise RuntimeError("Release tag differs from the requested 端云协同 version.")
        assets = release.get("assets", [])
        stem = f"wefto-{release['tag_name'][7:]}-{system}-{arch}"
        required = [stem + ".tar.gz", stem + ".manifest.json", "SHA256SUMS"]
        if any(a["name"] == required[0] for a in assets):
            if any(sum(a["name"] == name for a in assets) != 1 for name in required):
                raise RuntimeError("Platform archive exists but release assets are incomplete or duplicated.")
            return {"action": "release-package", "tag": release["tag_name"],
                    "version": release["tag_name"][7:], "os": system, "architecture": arch,
                    "releaseURL": release["html_url"], "stem": stem,
                    "assets": [{k: a.get(k) for k in ("name", "size", "digest", "url", "browser_download_url")}
                               for name in required for a in assets if a["name"] == name]}
    commit = api("/commits/" + urllib.parse.quote(ref, safe=""))["sha"]
    if not re.fullmatch(r"[0-9a-f]{40}", commit):
        raise RuntimeError("GitHub returned an invalid source commit.")
    return {"action": "source-build", "commit": commit, "sourceRef": ref,
            "version": tag[7:] if tag else "source-" + commit[:12], "os": system, "architecture": arch}


def download_bundle(info):
    home = Path.home()
    cache_base = home / "Library/Caches" if info["os"] == "darwin" else Path(os.environ.get("XDG_CACHE_HOME") or home / ".cache")
    cache = plain(cache_base / "api-router-start/downloads")
    cache.mkdir(parents=True, exist_ok=True, mode=0o700)
    directory = Path(tempfile.mkdtemp(prefix=info["tag"] + "-", dir=cache))
    for asset in info["assets"]:
        name = asset["name"]
        expected = "https://github.com/XHToken/magpie/releases/download/" + urllib.parse.quote(info["tag"], safe="") + "/" + urllib.parse.quote(name, safe="")
        if asset.get("browser_download_url") != expected or "/" in name or "\\" in name:
            raise RuntimeError("Unexpected release asset origin or path.")
        if not isinstance(asset.get("size"), int) or not 0 < asset["size"] <= 268435456:
            raise RuntimeError("Invalid release asset size.")
        urls = [(expected, "application/octet-stream")]
        api_url = asset.get("url", "")
        if re.fullmatch(re.escape(API) + r"/releases/assets/[0-9]+", api_url):
            urls.append((api_url, "application/octet-stream"))
        failure = None
        for url, accept in urls:
            try:
                request = urllib.request.Request(url, headers={"User-Agent": "api-router-start-skill", "Accept": accept})
                with urllib.request.urlopen(request, timeout=30) as response, (directory / name).open("wb") as output:
                    total = 0
                    while True:
                        block = response.read(1024 * 1024)
                        if not block:
                            break
                        total += len(block)
                        if total > asset["size"]:
                            raise RuntimeError("Release asset exceeds its declared size: " + name)
                        output.write(block)
                if total != asset["size"]:
                    raise RuntimeError("Release asset size mismatch: " + name)
                digest = asset.get("digest")
                if digest and digest != "sha256:" + file_hash(directory / name):
                    raise RuntimeError("GitHub asset digest mismatch: " + name)
                failure = None
                break
            except (OSError, urllib.error.URLError) as error:
                failure = error  # One retry through this same repository's asset API.
        if failure:
            raise RuntimeError(f"Download failed for {name}: {failure}. No source-build fallback was attempted.")
    verify_bundle(directory, info)
    return directory


def file_hash(path):
    digest = hashlib.sha256()
    with Path(path).open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def verify_bundle(directory, info):
    directory = plain(directory)
    sums_path = plain(directory / "SHA256SUMS")
    if sums_path.stat().st_size > 1024 * 1024:
        raise RuntimeError("SHA256SUMS exceeds its size bound.")
    hashes = {}
    for line in sums_path.read_text().splitlines():
        if not line.strip():
            continue
        match = re.fullmatch(r"([0-9a-f]{64}) [ *]([^/\\]+)", line)
        if not match or match[2] in hashes:
            raise RuntimeError("Invalid or duplicate SHA256SUMS entry.")
        hashes[match[2]] = match[1]
    for name in (info["stem"] + ".tar.gz", info["stem"] + ".manifest.json"):
        if name not in hashes or file_hash(plain(directory / name)) != hashes[name]:
            raise RuntimeError("SHA256SUMS mismatch: " + name)
    manifest_path = directory / (info["stem"] + ".manifest.json")
    if manifest_path.stat().st_size > 65536:
        raise RuntimeError("Manifest exceeds its size bound.")
    manifest = json.loads(manifest_path.read_text())
    if any(manifest.get(k) != v for k, v in {
        "schemaVersion": 1, "product": "wefto", "version": info["version"],
        "os": info["os"], "architecture": info["architecture"]}.items()):
        raise RuntimeError("Manifest identity, version or platform mismatch.")
    package = manifest["package"]
    if package["name"] != info["stem"] + ".tar.gz" or package["sha256"] != hashes[package["name"]] or package["size"] != (directory / package["name"]).stat().st_size:
        raise RuntimeError("Manifest package hash or size mismatch.")
    wanted = {"LICENSE": "0o644", "README.md": "0o644", "start.sh": "0o755", "wefto": "0o755"}
    if info["os"] == "darwin":
        wanted["start.command"] = "0o755"
    files = manifest["files"]
    if len(files) != len(wanted) or {f["name"] for f in files} != set(wanted):
        raise RuntimeError("Unexpected or duplicate manifest files.")
    for pin in files:
        if pin.get("mode") != wanted[pin["name"]] or not isinstance(pin.get("size"), int) or not 0 < pin["size"] <= 268435456 or not re.fullmatch(r"[0-9a-f]{64}", pin.get("sha256", "")):
            raise RuntimeError("Invalid manifest file pin.")
    if sum(f["size"] for f in files) > 268435456:
        raise RuntimeError("Manifest expands beyond its size bound.")
    return manifest


def extract_package(directory, destination, info):
    manifest = verify_bundle(directory, info)
    pins = {f["name"]: f for f in manifest["files"]}
    seen = set()
    with tarfile.open(directory / manifest["package"]["name"], "r:gz") as archive:
        for member in archive:
            name = member.name.removeprefix(info["stem"] + "/")
            if not member.isfile() or member.name != info["stem"] + "/" + name or name not in pins or name in seen:
                raise RuntimeError("Archive contains an unexpected path, duplicate or link.")
            pin = pins[name]
            if member.size != pin["size"] or member.mode != int(pin["mode"], 8):
                raise RuntimeError("Archive size or permissions differ from manifest.")
            seen.add(name)
            target = plain(destination / name)
            with archive.extractfile(member) as source, target.open("xb") as output:
                shutil.copyfileobj(source, output, 1024 * 1024)
            if file_hash(target) != pin["sha256"]:
                raise RuntimeError("Extracted file SHA256 mismatch: " + name)
            target.chmod(int(pin["mode"], 8))
    if seen != set(pins):
        raise RuntimeError("Archive is missing manifest files.")
    with (destination / "wefto").open("rb") as stream:
        header = stream.read(64)
    if info["os"] == "linux":
        machine = 62 if info["architecture"] == "amd64" else 183
        valid = len(header) >= 20 and header[:6] == b"\x7fELF\x02\x01" and struct.unpack_from("<H", header, 18)[0] == machine
    else:
        cpu = 0x1000007 if info["architecture"] == "amd64" else 0x100000C
        valid = len(header) >= 8 and header[:4] == b"\xcf\xfa\xed\xfe" and struct.unpack_from("<I", header, 4)[0] == cpu
    if not valid:
        raise RuntimeError("Executable format or CPU architecture mismatch.")
    return manifest


def prepare_package(target, info, bundle=None):
    if target.exists():
        raise RuntimeError("Install directory already exists; existing files preserved.")
    bundle = bundle or download_bundle(info)
    target.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix=".wefto-unpack-", dir=target.parent) as tmp:
        stage = Path(tmp) / "install"
        stage.mkdir(mode=0o700)
        extract_package(bundle, stage, info)
        if run([stage / "wefto", "version"]) != info["version"]:
            raise RuntimeError("Package executable reports another version.")
        # Keep provenance without saving the entire GitHub response.
        (stage / ".wefto-package.json").write_text(json.dumps({k: info[k] for k in (
            "tag", "version", "os", "architecture", "releaseURL")}, indent=2) + "\n")
        os.rename(stage, target)
    info["directory"] = str(bundle)


def prepare(target, info):
    if info["action"] == "release-package":
        return prepare_package(target, info)
    if info["action"] != "source-build":
        raise RuntimeError("Unknown preparation mode.")
    missing = [name for name in ("git", "go") if not shutil.which(name)]
    if missing:
        raise RuntimeError("Source build requires " + ", ".join(missing) +
                           ". Provide these tools, then rerun; no system packages were changed.")
    if target.exists():
        raise RuntimeError("Install directory exists without a reusable binary. Choose a new empty --install-dir; existing files preserved.")
    target.parent.mkdir(parents=True, exist_ok=True)
    # Private staging on the same filesystem; publish only after a successful build.
    with tempfile.TemporaryDirectory(prefix=".wefto-build-", dir=str(target.parent)) as tmp:
        root = Path(tmp)
        source = root / "source"
        run(["git", "init", "--quiet", source])
        run(["git", "-C", source, "remote", "add", "origin", REPO])
        run(["git", "-C", source, "fetch", "--quiet", "--depth=1", "origin", info["commit"]])
        run(["git", "-C", source, "checkout", "--quiet", "--detach", "FETCH_HEAD"])
        if run(["git", "-C", source, "rev-parse", "HEAD"]) != info["commit"]:
            raise RuntimeError("Fetched source differs from the pinned commit.")
        module = source / "magpie"
        required = re.search(r"^go\s+(\S+)", (module / "go.mod").read_text(), re.M)
        if not required:
            raise RuntimeError("No Go requirement in product go.mod.")
        info["requiredGo"] = required[1]
        print(f"Building 端云协同 {info['commit']} (Go {required[1]})", file=sys.stderr)
        publish = root / "install"
        publish.mkdir(mode=0o700)
        exe = publish / "wefto"
        cgo = "1" if info["os"] == "darwin" and shutil.which("cc") else "0"
        build_env = dict(os.environ, CGO_ENABLED=cgo, GOTOOLCHAIN="auto")
        build_env.pop("GOOS", None)
        build_env.pop("GOARCH", None)
        cmd_pkg = "./cmd/wefto" if (module / "cmd" / "wefto").is_dir() else "./cmd/wefto"
        run(["go", "-C", module, "build", "-trimpath", "-buildvcs=false",
             "-ldflags", "-X main.version=" + info["version"], "-o", exe, cmd_pkg], env=build_env)
        if run([exe, "version"]) != info["version"]:
            raise RuntimeError("Built product version differs from selected source.")
        exe.chmod(0o700)
        info["sha256"] = hashlib.sha256(exe.read_bytes()).hexdigest()
        (publish / ".wefto-source.json").write_text(json.dumps(info, indent=2) + "\n")
        os.rename(publish, target)


def local_json(url, token=None):
    # No proxy or redirects for requests carrying the management credential.
    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, *args, **kwargs):
            raise RuntimeError("Unexpected management redirect.")
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    headers = {"Authorization": "Bearer " + token} if token else {}
    with opener.open(urllib.request.Request(url, headers=headers), timeout=5) as response:
        return json.load(response)


def verified(exe, layout, status):
    product = "api-router" if exe.name == "api-router" else "wefto"
    if status["executable"] != str(exe):
        raise RuntimeError("Another executable owns this layout; use its own CLI. No process changed.")
    address = status["address"]
    origin = "http://" + address + "/"
    uri = urllib.parse.urlsplit(origin)
    if not ipaddress.ip_address(uri.hostname).is_loopback or not uri.port or uri.username or uri.password or uri.path != "/" or uri.query or uri.fragment:
        raise RuntimeError("Refusing a non-loopback management address.")
    saved = json.loads(plain(Path(layout["config"]) / "config.json").read_text())
    if saved["listen"]["address"] != address:
        raise RuntimeError("Saved listen address differs from the running instance.")
    health = local_json(origin + "healthz")
    if health.get("service") not in (product, "api-router") or health.get("status") != "ok" or health.get("pid") != status["pid"]:
        raise RuntimeError("Health endpoint identity mismatch.")
    token_file = plain(Path(layout["state"]) / "router.lock" / "management-token")
    live = local_json(origin + "api/service", token_file.read_text().strip())
    if any(live.get(key) != status.get(key) for key in ("pid", "executable", "instanceId", "processStamp")):
        raise RuntimeError("Authenticated service identity mismatch.")
    if live.get("service") not in (product, "api-router") or not live.get("running"):
        raise RuntimeError("端云协同 did not report running.")
    return {"version": live["version"], "executable": str(exe), "pid": live["pid"],
            "managementURL": origin, "config": layout["config"], "data": layout["data"]}


def start(exe, system, check=False):
    layout = router_json(exe, "dirs")
    if system in ("linux", "darwin"):
        try:
            status = router_json(exe, "service", "status" if check else "start")
            if not status.get("running"):
                raise RuntimeError("端云协同 is stopped.")
            return verified(exe, layout, status)
        except Exception:
            if system == "linux" or check:
                raise
    # Fallback for older Darwin builds without native external process control.
    # Reuse a live, authenticated record; if a stale lock belongs to an exited
    # local process, only proceed when the lock is absent or already archived.
    record = plain(Path(layout["state"]) / "router.lock" / "instance.json")
    lock = record.parent
    if lock.exists():
        if not record.is_file():
            raise RuntimeError("Unknown service lock; inspect it without deleting it.")
        saved_record = json.loads(record.read_text())
        try:
            return verified(exe, layout, saved_record)
        except (OSError, urllib.error.URLError, RuntimeError):
            pid = int(saved_record.get("pid") or 0)
            if pid > 0:
                try:
                    os.kill(pid, 0)
                    raise
                except ProcessLookupError:
                    archive = plain(Path(layout["state"])) / f"router.lock.exited-{time.time_ns()}"
                    os.rename(lock, archive)
            else:
                raise
    if check:
        raise RuntimeError("端云协同 is stopped.")
    logs = plain(Path(layout["logs"]))
    logs.mkdir(parents=True, exist_ok=True, mode=0o700)
    log_path = plain(logs / ("skill-start-" + str(time.time_ns()) + ".log"))
    with log_path.open("xb") as log:
        os.chmod(log_path, 0o600)
        launcher = plain(exe.parent / "start.sh")
        command = [str(launcher)] if launcher.is_file() else [str(exe), "serve"]
        child = subprocess.Popen(command, stdin=subprocess.DEVNULL,
                                 stdout=log, stderr=log, start_new_session=True)
    for _ in range(100):
        if child.poll() is not None:
            raise RuntimeError(f"端云协同 exited ({child.returncode}); inspect private log: {log_path}")
        if record.is_file():
            status = json.loads(record.read_text())
            if status.get("pid") != child.pid:
                raise RuntimeError("Concurrent startup owns this record; no other process was stopped.")
            try:
                return verified(exe, layout, status)
            except (OSError, urllib.error.URLError):
                pass  # Record is written before the HTTP listener becomes ready.
        time.sleep(0.2)
    raise RuntimeError(f"Startup not verified within 20s; inspect PID {child.pid} and private log {log_path}. No lock removed.")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tag")
    parser.add_argument("--install-dir")
    modes = parser.add_mutually_exclusive_group()
    modes.add_argument("--plan", action="store_true")
    modes.add_argument("--prepare-only", action="store_true")
    modes.add_argument("--check-only", action="store_true")
    modes.add_argument("--download-only", action="store_true")
    parser.add_argument("--stable-only", action="store_true")
    parser.add_argument("--no-browser", action="store_true")
    args = parser.parse_args()
    system, arch = platform_id()
    target = plain(args.install_dir or Path.home() / ".local/opt/wefto")
    validate_target(target, system)
    exe = plain(target / "wefto")
    legacy = Path.home() / ".local/opt/api-router/api-router"
    if not args.install_dir and not args.tag and not args.download_only and not exe.exists() and legacy.is_file():
        exe = plain(legacy)
        target = exe.parent
        validate_target(target, system)
    if exe.exists() and not args.download_only:
        if args.tag and (not TAG.fullmatch(args.tag) or run([exe, "version"]) != args.tag[7:]):
            raise RuntimeError("Different installed version; follow references/update.md. Existing installation preserved.")
        info = {"action": "reuse", "executable": str(exe)}
    else:
        if args.check_only:
            raise RuntimeError("No installed 端云协同 to check.")
        info = discover(system, arch, args.tag, args.stable_only)
        info["installDir"] = str(target)
        if args.download_only:
            if info["action"] != "release-package":
                raise RuntimeError("No compatible prebuilt package to download. Omit --download-only to build from source.")
            info["directory"] = str(download_bundle(info))
        elif not args.plan:
            prepare(target, info)
    if not (args.plan or args.prepare_only or args.download_only):
        info.update(start(exe, system, args.check_only))
    print(json.dumps(info, ensure_ascii=False, indent=2))
    if not (args.plan or args.prepare_only or args.check_only or args.download_only or args.no_browser):
        webbrowser.open(info["managementURL"])


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, KeyError, TypeError, tarfile.TarError, RuntimeError, subprocess.SubprocessError) as error:
        print("api-router-start: " + str(error), file=sys.stderr)
        sys.exit(1)
