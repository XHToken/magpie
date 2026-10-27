"""Behavior checks. --integration downloads beta.4 and starts it in isolated XDG roots."""
import hashlib
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import struct
import sys
import tempfile
import tarfile
import unittest
from unittest.mock import patch

sys.dont_write_bytecode = True
SCRIPT = Path(__file__).resolve().parents[2] / "skills/api-router-start/scripts/prepare-unix.py"
spec = importlib.util.spec_from_file_location("router_skill", SCRIPT)
skill = importlib.util.module_from_spec(spec)
spec.loader.exec_module(skill)


class SelectionTests(unittest.TestCase):
    def test_install_must_not_overlap_user_data(self):
        with tempfile.TemporaryDirectory() as tmp:
            data = Path(tmp) / "data"
            with patch.dict(os.environ, {"XDG_DATA_HOME": str(data)}):
                with self.assertRaises(RuntimeError):
                    skill.validate_target(data / "wefto/program", "linux")

    def test_wrong_product_never_downloaded(self):
        with patch.object(skill, "api") as fetch:
            with self.assertRaises(RuntimeError):
                skill.discover("linux", "amd64", "api-router-v0.1.0")
            fetch.assert_not_called()

    def test_source_commit_is_pinned(self):
        commit = "a" * 40
        with patch.object(skill, "api", side_effect=[[], {"sha": commit}]) as fetch:
            result = skill.discover("linux", "amd64", None)
        self.assertEqual(result["commit"], commit)
        self.assertEqual(result["action"], "source-build")
        self.assertEqual(fetch.call_args.args[0], "/commits/main")

    def test_all_unix_platforms_select_binaries_without_source_queries(self):
        assets = [{"name": "SHA256SUMS"}]
        for system in ("darwin", "linux"):
            for arch in ("amd64", "arm64"):
                for suffix in (".tar.gz", ".manifest.json"):
                    assets.append({"name": f"wefto-1.0.0-{system}-{arch}{suffix}"})
        release = {"tag_name": "wefto-v1.0.0", "html_url": "release-url", "assets": assets}
        for system in ("darwin", "linux"):
            for arch in ("amd64", "arm64"):
                with self.subTest(system=system, arch=arch), patch.object(skill, "api", return_value=[release]) as fetch:
                    result = skill.discover(system, arch, None)
                    self.assertEqual(result["action"], "release-package")
                    self.assertEqual(result["stem"], f"wefto-1.0.0-{system}-{arch}")
                    self.assertEqual(fetch.call_count, 1)

    def test_incomplete_archive_never_falls_back_to_source(self):
        release = {"tag_name": "wefto-v1.0.0", "html_url": "release-url", "assets": [
            {"name": "wefto-1.0.0-linux-amd64.tar.gz"}]}
        with patch.object(skill, "api", return_value=[release]) as fetch:
            with self.assertRaises(RuntimeError):
                skill.discover("linux", "amd64", None)
            self.assertEqual(fetch.call_count, 1)

    def test_latest_compatible_release_is_selected_across_pages(self):
        def release(version, published, system="linux", draft=False, preview=False):
            stem = f"wefto-{version}-{system}-amd64"
            return {"tag_name": "wefto-v" + version, "published_at": published, "draft": draft,
                    "prerelease": preview, "html_url": "release-url", "assets": [
                        {"name": stem + ".tar.gz"}, {"name": stem + ".manifest.json"}, {"name": "SHA256SUMS"}]}
        old = release("0.1.0-beta.4", "2026-10-01T00:00:00Z", preview=True)
        newest = release("0.2.0-beta.1", "2026-10-26T00:00:00Z", preview=True)
        draft = release("99.0.0", "2026-10-28T00:00:00Z", draft=True)
        other_platform = release("2.0.0", "2026-10-27T00:00:00Z", system="darwin")
        filler = [{"tag_name": "other-product", "draft": False}] * 99
        with patch.object(skill, "api", side_effect=[[old] + filler, [newest, draft, other_platform]]) as fetch:
            result = skill.discover("linux", "amd64", None)
        self.assertEqual(result["tag"], newest["tag_name"])
        self.assertEqual(fetch.call_count, 2)
        self.assertTrue(all('/releases?' in c.args[0] for c in fetch.call_args_list))

    def test_stable_filter_and_explicit_tag_are_respected(self):
        def release(version, date, preview):
            stem = f"wefto-{version}-linux-amd64"
            return {"tag_name": "wefto-v" + version, "published_at": date, "prerelease": preview,
                    "html_url": "release-url", "assets": [{"name": stem + s} for s in (".tar.gz", ".manifest.json")] + [{"name": "SHA256SUMS"}]}
        stable = release("1.0.0", "2026-10-01T00:00:00Z", False)
        preview = release("1.1.0-beta.1", "2026-10-26T00:00:00Z", True)
        with patch.object(skill, "api", return_value=[stable, preview]):
            self.assertEqual(skill.discover("linux", "amd64", None, True)["tag"], stable["tag_name"])
        with patch.object(skill, "api", return_value=stable) as fetch:
            self.assertEqual(skill.discover("linux", "amd64", stable["tag_name"])["tag"], stable["tag_name"])
        self.assertEqual(fetch.call_args.args[0], "/releases/tags/" + stable["tag_name"])

    def test_rosetta_selects_native_arm64(self):
        with patch.object(skill.platform, "system", return_value="Darwin"), patch.object(skill.platform, "machine", return_value="x86_64"), patch.object(skill.platform, "mac_ver", return_value=("12.0", (), "")), patch.object(skill, "run", return_value="1"):
            self.assertEqual(skill.platform_id(), ("darwin", "arm64"))

    def test_redirect_does_not_forward_credential(self):
        import http.server
        import threading
        requests = []

        class Handler(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                requests.append(self.path)
                self.send_response(302)
                self.send_header("Location", "/credential-target")
                self.end_headers()

            def log_message(self, *args):
                pass

        with http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler) as server:
            thread = threading.Thread(target=server.serve_forever, daemon=True)
            thread.start()
            try:
                with self.assertRaises(RuntimeError):
                    skill.local_json(f"http://127.0.0.1:{server.server_port}/api/service", "test-secret")
                self.assertEqual(requests, ["/api/service"])
            finally:
                server.shutdown()
                thread.join()


class ArchiveTests(unittest.TestCase):
    def bundle(self, directory, system="linux", arch="amd64", bad_path=None, wrong_cpu=False, link=False):
        info = {"action": "release-package", "tag": "wefto-v1.0.0", "version": "1.0.0", "os": system,
                "architecture": arch, "releaseURL": "release-url", "stem": f"wefto-1.0.0-{system}-{arch}"}
        header = bytearray(64)
        if system == "linux":
            header[:6] = b"\x7fELF\x02\x01"
            struct.pack_into("<H", header, 18, 0 if wrong_cpu else (62 if arch == "amd64" else 183))
        else:
            header[:4] = b"\xcf\xfa\xed\xfe"
            struct.pack_into("<I", header, 4, 0 if wrong_cpu else (0x1000007 if arch == "amd64" else 0x100000C))
        content = {"LICENSE": b"license", "README.md": b"readme", "start.sh": b"#!/bin/sh\nexec ./wefto serve\n", "wefto": bytes(header)}
        if system == "darwin":
            content["start.command"] = content["start.sh"]
        pins = []
        archive_path = directory / (info["stem"] + ".tar.gz")
        with tarfile.open(archive_path, "w:gz") as archive:
            for name, data in content.items():
                mode = 0o755 if name in ("wefto", "start.sh", "start.command") else 0o644
                pins.append({"name": name, "size": len(data), "sha256": hashlib.sha256(data).hexdigest(), "mode": oct(mode)})
                member = tarfile.TarInfo(info["stem"] + "/" + (bad_path if name == "wefto" and bad_path else name))
                member.mode = mode
                member.size = len(data)
                if link and name == "wefto":
                    member.type = tarfile.SYMTYPE
                    member.linkname = "/outside"
                    member.size = 0
                archive.addfile(member, io.BytesIO(data) if member.isfile() else None)
        manifest = {"schemaVersion": 1, "product": "wefto", "version": "1.0.0", "os": system, "architecture": arch,
                    "package": {"name": archive_path.name, "size": archive_path.stat().st_size, "sha256": skill.file_hash(archive_path)}, "files": pins}
        manifest_path = directory / (info["stem"] + ".manifest.json")
        manifest_path.write_text(json.dumps(manifest))
        (directory / "SHA256SUMS").write_text("".join(skill.file_hash(p) + "  " + p.name + "\n" for p in (archive_path, manifest_path)))
        return info

    def test_four_platform_archives_extract_with_permissions(self):
        for system in ("darwin", "linux"):
            for arch in ("amd64", "arm64"):
                with self.subTest(system=system, arch=arch), tempfile.TemporaryDirectory() as tmp:
                    root = Path(tmp)
                    info = self.bundle(root, system, arch)
                    output = root / "out"
                    output.mkdir()
                    skill.extract_package(root, output, info)
                    self.assertTrue((output / "start.sh").is_file())
                    if os.name != "nt":
                        self.assertEqual((output / "wefto").stat().st_mode & 0o777, 0o755)

    def test_tampered_download_is_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            info = self.bundle(root)
            with (root / (info["stem"] + ".tar.gz")).open("ab") as stream:
                stream.write(b"tampered")
            with self.assertRaises(RuntimeError):
                skill.verify_bundle(root, info)

    def test_download_retries_same_repository_api_and_checks_hashes(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            info = self.bundle(root)
            names = [info["stem"] + ".tar.gz", info["stem"] + ".manifest.json", "SHA256SUMS"]
            info["assets"] = [{"name": name, "size": (root / name).stat().st_size, "digest": "sha256:" + skill.file_hash(root / name),
                               "browser_download_url": f"https://github.com/XHToken/magpie/releases/download/{info['tag']}/{name}",
                               "url": skill.API + "/releases/assets/" + str(i + 1)} for i, name in enumerate(names)]
            replies = [skill.urllib.error.URLError("network unavailable"), io.BytesIO((root / names[0]).read_bytes()),
                       io.BytesIO((root / names[1]).read_bytes()), io.BytesIO((root / names[2]).read_bytes())]
            with patch.dict(os.environ, {"XDG_CACHE_HOME": str(root / "cache")}), patch.object(skill.urllib.request, "urlopen", side_effect=replies) as request:
                downloaded = skill.download_bundle(info)
            self.assertEqual(skill.verify_bundle(downloaded, info)["version"], "1.0.0")
            self.assertEqual(request.call_args_list[1].args[0].full_url, skill.API + "/releases/assets/1")

    def test_traversal_links_and_wrong_cpu_are_rejected(self):
        for options in ({"bad_path": "../../outside"}, {"link": True}, {"wrong_cpu": True}):
            with self.subTest(options=options), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                info = self.bundle(root, **options)
                output = root / "out"
                output.mkdir()
                with self.assertRaises(RuntimeError):
                    skill.extract_package(root, output, info)
                self.assertFalse((root.parent / "outside").exists())


def integration():
    if skill.platform_id()[0] != "linux":
        raise RuntimeError("Integration runner uses Linux; macOS requires native acceptance separately.")
    with tempfile.TemporaryDirectory(prefix="wefto-skill-qa-") as tmp:
        root = Path(tmp)
        env = dict(os.environ)
        for key, dirname in (("XDG_CONFIG_HOME", "config"), ("XDG_DATA_HOME", "data"),
                             ("XDG_STATE_HOME", "state"), ("XDG_CACHE_HOME", "cache")):
            env[key] = str(root / dirname)
        target = root / "program"
        exe = target / "wefto"
        command = [sys.executable, str(SCRIPT), "--tag", "wefto-v0.1.0-beta.4", "--install-dir", str(target), "--no-browser"]
        def invoke(arguments):
            release_file = os.environ.get("WEFTO_SKILL_TEST_RELEASE")
            bundle_dir = os.environ.get("WEFTO_SKILL_TEST_BUNDLE")
            if not (release_file and bundle_dir):
                return json.loads(subprocess.check_output(arguments, text=True, env=env))
            # Replay captured official metadata and files when GitHub is rate
            # limited. Installation, hashing and real process startup stay live.
            capture = io.StringIO()
            with patch.dict(os.environ, env, clear=True), patch.object(sys, "argv", arguments[1:]), patch.object(skill, "api", return_value=json.loads(Path(release_file).read_text())), patch.object(skill, "download_bundle", return_value=Path(bundle_dir)), contextlib.redirect_stdout(capture):
                skill.main()
            return json.loads(capture.getvalue())
        try:
            prepared = invoke(command + ["--prepare-only"])
            assert prepared["action"] == "release-package" and prepared["version"] == "0.1.0-beta.4"
            started = invoke(command)
            assert started["config"] == str(root / "config/wefto")
            again = invoke(command)
            assert started["pid"] == again["pid"]
            checked = invoke(command + ["--check-only"])
            assert checked["pid"] == started["pid"]
            subprocess.check_call([str(exe), "service", "stop"], env=env, stdout=subprocess.DEVNULL)
            # Exercise the Darwin serve launcher with a real binary, including
            # authenticated readiness and reuse. This is not native macOS QA.
            with patch.dict(os.environ, env, clear=True):
                darwin = skill.start(exe, "darwin")
                assert skill.start(exe, "darwin")["pid"] == darwin["pid"]
            print("PASS: verified prebuilt Linux package, version, isolated dirs, Linux start/reuse/status, package start.sh launcher flow on Linux.")
        finally:
            if exe.exists():
                subprocess.run([str(exe), "service", "stop"], env=env, stdout=subprocess.DEVNULL, check=True)


if __name__ == "__main__":
    if "--integration" in sys.argv:
        integration()
    else:
        unittest.main()
