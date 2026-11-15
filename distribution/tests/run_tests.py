#!/usr/bin/env python3
"""Exercise the actual installer and CLI against generated offline releases."""
import hashlib
import http.server
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import sys
import tempfile
import threading

ROOT = Path(__file__).resolve().parents[2]
TARGETS = ("linux-x86_64", "linux-aarch64", "macos-x86_64", "macos-aarch64")
TARGET = ("macos" if sys.platform == "darwin" else "linux") + "-" + {
    "arm64": "aarch64", "aarch64": "aarch64", "x86_64": "x86_64", "AMD64": "x86_64"
}[platform.machine()]


def run(args, env=None, cwd=ROOT, ok=True, timeout=60):
    result = subprocess.run([str(x) for x in args], env=env, cwd=cwd,
                            text=True, capture_output=True, timeout=timeout)
    if ok and result.returncode:
        raise AssertionError(f"{args}: {result.stdout}\n{result.stderr}")
    return result


def release(tree, tag, program):
    dest = tree / "download" / tag
    dest.mkdir(parents=True, exist_ok=True)
    for target in TARGETS:
        (dest / f"lotel-{target}").write_bytes(program)
    manifest = dest / "lotel-manifest.json"
    run([sys.executable, ROOT / "distribution/manifest.py", "--tag", tag,
         "--base-url", tree.as_uri(), "--dir", dest, "--output", manifest])
    latest = tree / "latest/download"
    latest.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(manifest, latest / manifest.name)
    return json.loads(manifest.read_text())


def installer_tests(work):
    tree = work / "installer-releases"
    program = b"#!/bin/sh\nprintf 'lotel v0.0.1\\n'\n"
    manifest = release(tree, "v0.0.1", program)
    install_dir = work / "install space"
    env = dict(os.environ, LOTEL_RELEASE_BASE_URL=tree.as_uri(),
               LOTEL_INSTALL_DIR=str(install_dir))
    run(["sh", ROOT / "distribution/install.sh"], env)
    installed = install_dir / "lotel"
    assert run([installed, "--version"]).stdout == "lotel v0.0.1\n"
    env["LOTEL_INSTALL_VERSION"] = "v0.0.1"
    run(["sh", ROOT / "distribution/install.sh"], env)
    del env["LOTEL_INSTALL_VERSION"]
    for name in ("dollar$HOME", "space apostrophe' `echo wrong` \\slash", "trailing\n"):
        quoted_install = work / name
        result = run(["sh", ROOT / "distribution/install.sh"], dict(env, LOTEL_INSTALL_DIR=str(quoted_install)))
        guidance = result.stderr[result.stderr.index("  export PATH="):].split('\n', 1)[0]
        if "\n" in name:
            guidance = result.stderr[result.stderr.index("  export PATH="):].split(':"$PATH"', 1)[0] + ':"$PATH"'
        expanded = run(["sh", "-c", guidance + "; printf '%s' \"$PATH\""], env).stdout
        assert expanded.split(os.pathsep)[0] == str(quoted_install), (name, guidance, expanded)
    original = installed.read_bytes()
    latest = tree / "latest/download/lotel-manifest.json"
    for fault in ("schema", "tag", "url", "digest", "content", "version"):
        modified = json.loads(json.dumps(manifest))
        (tree / "download/v0.0.1" / f"lotel-{TARGET}").write_bytes(program)
        if fault == "schema":
            modified["schema"] = 2
        elif fault == "tag":
            modified["tag"] = "v0.0.2"
        elif fault == "url":
            modified["assets"][TARGET] = "https://example.com/unexpected"
        elif fault == "digest":
            modified["sha256"][TARGET] = "invalid"
        elif fault == "content":
            modified["sha256"][TARGET] = "0" * 64
        else:
            wrong = b"#!/bin/sh\necho wrong\n"
            (tree / "download/v0.0.1" / f"lotel-{TARGET}").write_bytes(wrong)
            modified["sha256"][TARGET] = hashlib.sha256(wrong).hexdigest()
        latest.write_text(json.dumps(modified, indent=2) + "\n")
        result = run(["sh", ROOT / "distribution/install.sh"], env, ok=False)
        assert result.returncode, fault
        assert installed.read_bytes() == original, fault
        assert not list(install_dir.glob(".lotel-*")), fault
    release(tree, "v0.0.1", b"#!/bin/sh\ntrap '' TERM\nwhile :; do :; done\n")
    result = run(["sh", ROOT / "distribution/install.sh"], env, ok=False, timeout=15)
    assert result.returncode and installed.read_bytes() == original
    assert not list(install_dir.glob(".lotel-*"))
    release(tree, "v0.0.1", program)
    unsupported = work / "unsupported"
    unsupported.mkdir()
    (unsupported / "uname").write_text("#!/bin/sh\necho FreeBSD\n")
    (unsupported / "uname").chmod(0o755)
    unsupported_env = dict(env, PATH=str(unsupported) + os.pathsep + os.environ["PATH"])
    assert run(["sh", ROOT / "distribution/install.sh"], unsupported_env, ok=False).returncode
    blocked = work / "directory-target"
    (blocked / "lotel").mkdir(parents=True)
    assert run(["sh", ROOT / "distribution/install.sh"], dict(env, LOTEL_INSTALL_DIR=str(blocked)), ok=False).returncode
    default_env = dict(env, HOME=str(work / "default-home"))
    del default_env["LOTEL_INSTALL_DIR"]
    run(["sh", ROOT / "distribution/install.sh"], default_env)
    assert (work / "default-home/.local/bin/lotel").is_file()
    env["LOTEL_INSTALL_VERSION"] = "v01.0.0"
    assert run(["sh", ROOT / "distribution/install.sh"], env, ok=False).returncode
    print("PASS installer latest, pin, paths with spaces, invalid metadata, checksum, version, bounded probe, cleanup")


class Health(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.end_headers()
    def log_message(self, *args):
        pass


def build_binary(path, tag, health_url, provenance="release"):
    run(["go", "build", "-ldflags",
         f"-X main.version={tag} -X main.distribution={provenance} "
         f"-X github.com/mattsp1290/lotel/internal/collector.healthURL={health_url}",
         "-o", path, "./cmd/lotel"], timeout=180)
    assert run([path, "--version"]).stdout == f"lotel {tag}\n"


def cli_tests(work):
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Health)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    try:
        health_url = f"http://127.0.0.1:{server.server_port}/"
        old, new = work / "old", work / "new"
        build_binary(old, "v0.0.1", health_url)
        build_binary(new, "v0.0.2", health_url)
        tree = work / "cli-releases"
        release(tree, "v0.0.1", old.read_bytes())
        release(tree, "v0.0.2", new.read_bytes())
        bindir = work / "fake-bin"
        bindir.mkdir()
        docker = bindir / "docker"
        docker.write_text(f"#!{sys.executable}\n" + '''import json, os, pathlib, subprocess, sys
home = pathlib.Path(os.environ["HOME"])
marker = home / "running"
args = sys.argv[1:]
version = subprocess.check_output([os.environ["TEST_LOTEL"], "--version"], text=True).strip()
with (home / "docker-calls.jsonl").open("a") as f:
    f.write(json.dumps({"args": args, "version": version}) + "\\n")
if os.environ.get("TEST_DOCKER_ERROR"):
    print("Cannot connect to the Docker daemon", file=sys.stderr)
    sys.exit(125)
if args[0] == "ps":
    if os.environ.get("TEST_BAD_LIST"): print("invalid-container")
    elif marker.exists(): print("0123456789ab")
elif args[0] == "inspect":
    if not marker.exists(): sys.exit(1)
    if "-f" in args or "--format" in args: print("unknown" if os.environ.get("TEST_BAD_STATE") else "true")
elif args[0] in ("stop", "rm"):
    marker.unlink(missing_ok=True)
elif args[0] == "run":
    if os.environ.get("TEST_FAIL_START"): sys.exit(42)
    marker.write_text("running")
    print("fake-container")
else:
    sys.exit(1)
''')
        docker.chmod(0o755)
        for case in ("custom", "managed", "stopped", "restart-failure", "docker-error", "malformed-list", "malformed-state"):
            home = work / case
            home.mkdir()
            state_dir = home / ".lotel"
            state_dir.mkdir()
            data = state_dir / "data"
            data.mkdir()
            sentinel = data / "application.jsonl"
            sentinel.write_text("preserve application telemetry\n")
            custom = home / ("application/config-$LOTEL_REVIEW_VALUE-quote'`echo wrong`\\slash.yaml" if case == "restart-failure" else "application/lotel-collector.yaml")
            custom.parent.mkdir()
            custom.write_text("custom config preserved\n")
            config = custom
            if case == "managed":
                config = state_dir / "collector-config.yaml"
                config.write_text("# lotel-managed-config v1\nold defaults\n")
            elsewhere = home / "elsewhere"
            elsewhere.mkdir()
            (elsewhere / "lotel-collector.yaml").write_text("wrong config\n")
            state = state_dir / "collector.state"
            state.write_text(json.dumps({"container_id": "old-container", "container_name": "lotel-collector", "image": "old-image", "started_at": "2026-01-01T00:00:00Z", "config_path": str(config), "data_path": str(data)}))
            if case != "stopped":
                (home / "running").write_text("running")
            installed = home / ("lotel-$LOTEL_REVIEW_VALUE-quote'`echo wrong`" if case == "restart-failure" else "lotel")
            shutil.copyfile(old, installed)
            installed.chmod(0o755)
            env = dict(os.environ, HOME=str(home), PATH=str(bindir) + os.pathsep + os.environ["PATH"], LOTEL_RELEASE_BASE_URL=tree.as_uri(), TEST_LOTEL=str(installed))
            before = installed.read_bytes(), state.read_bytes(), custom.read_bytes(), sentinel.read_bytes()
            check = run([installed, "upgrade", "--check", "--json"], env, elsewhere)
            assert json.loads(check.stdout)["update_available"]
            assert not (home / "docker-calls.jsonl").exists()
            assert (installed.read_bytes(), state.read_bytes(), custom.read_bytes(), sentinel.read_bytes()) == before
            if case in ("docker-error", "malformed-list", "malformed-state"):
                env[{"docker-error": "TEST_DOCKER_ERROR", "malformed-list": "TEST_BAD_LIST", "malformed-state": "TEST_BAD_STATE"}[case]] = "1"
                failed = run([installed, "upgrade", "--json"], env, elsewhere, ok=False)
                assert failed.returncode and not json.loads(failed.stdout)["upgraded"]
                assert (installed.read_bytes(), state.read_bytes(), custom.read_bytes(), sentinel.read_bytes()) == before
                assert "collection will be unavailable" not in failed.stderr
                calls = [json.loads(line) for line in (home / "docker-calls.jsonl").read_text().splitlines()]
                assert all(c["args"][0] in ("ps", "inspect") for c in calls)
                continue
            if case == "restart-failure": env["TEST_FAIL_START"] = "1"
            result = run([installed, "upgrade", "--json"], env, elsewhere, ok=case != "restart-failure")
            report = json.loads(result.stdout)
            assert report["upgraded"] and report["collector_restarted"] == (case in ("custom", "managed")), (case, report, result.stderr)
            assert run([installed, "--version"]).stdout == "lotel v0.0.2\n"
            calls = [json.loads(line) for line in (home / "docker-calls.jsonl").read_text().splitlines()]
            if case != "stopped":
                assert result.stderr.index("collection will be unavailable") < result.stderr.index("Stopping collector")
                assert any(c["args"][0] == "stop" and c["version"] == "lotel v0.0.1" for c in calls)
                assert any(c["args"][0] == "run" and c["version"] == "lotel v0.0.2" for c in calls)
            if case in ("custom", "managed"):
                assert json.loads(state.read_text())["config_path"] == str(config)
                assert "collection has resumed" in result.stderr
            if case == "managed": assert "lotel-managed-config v" in config.read_text() and "old defaults" not in config.read_text()
            if case == "restart-failure":
                assert result.returncode and "--config" in result.stderr
                recovery = result.stderr.split("Run ", 1)[1].split(" to recover:", 1)[0]
                recovery_env = dict(env)
                del recovery_env["TEST_FAIL_START"]
                run(["sh", "-c", recovery], recovery_env, elsewhere)
                assert json.loads(state.read_text())["config_path"] == str(config)
            assert custom.read_bytes() == before[2] and sentinel.read_bytes() == before[3]
            previous_calls = (home / "docker-calls.jsonl").read_bytes()
            noop = run([installed, "upgrade", "--json"], env, elsewhere)
            assert not json.loads(noop.stdout)["upgraded"]
            assert (home / "docker-calls.jsonl").read_bytes() == previous_calls
            if case == "stopped":
                rollback = run([installed, "upgrade", "--version", "v0.0.1", "--json"], env, elsewhere)
                rollback_report = json.loads(rollback.stdout)
                assert rollback_report["upgraded"] and not rollback_report["update_available"]
                assert run([installed, "--version"]).stdout == "lotel v0.0.1\n"
        print("PASS real CLI check, upgrade, rollback, stopped-state preservation, config preservation, new-binary restart, recovery advice, no-op, JSON output")
    finally:
        server.shutdown()
        server.server_close()


def main():
    with tempfile.TemporaryDirectory(prefix="lotel-distribution-") as temporary:
        work = Path(temporary).resolve()
        installer_tests(work)
        cli_tests(work)


if __name__ == "__main__":
    main()
