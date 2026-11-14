"""Exercise the actual deployment script with local HTTP and Supervisor boundaries."""
import hashlib
import gzip
import http.server
import io
import json
import os
from pathlib import Path
import shlex
import subprocess
import tarfile
import tempfile
import threading
import unittest


SCRIPT = Path(__file__).with_name("deploy.sh")
OLD = "v2.0.1-alpha11"
NEW = "v2.0.1-alpha13"
ARCHIVE = "DraARL-linux-amd64.tar.gz"


def binary(version):
    return f'''#!/bin/sh
case "$*" in
  -h) echo '-migrate-only'; exit 0 ;;
  *-migrate-only*)
    test "$(cat "$DEPLOY_TEST_ROOT/running")" = STOPPED || exit 8
    echo invoked > "$DEPLOY_TEST_ROOT/migration-ran"
    if test -e "$DEPLOY_TEST_ROOT/migration-fails"; then
      echo partial-migration > "$DEPLOY_TEST_ROOT/database"
      exit 9
    fi
    echo migrated-database > "$DEPLOY_TEST_ROOT/database"
    exit 0 ;;
esac
printf "DraARL version {version} (build time: test)\\n"
'''.encode()


class DeploymentTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="draarl-deploy-test-")
        self.root = Path(self.temp.name)
        self.app = self.root / "app"
        self.app.mkdir()
        self.server = self.app / "server"
        self.server.write_bytes(binary(OLD))
        self.server.chmod(0o755)
        self.config = self.app / "config.yaml"
        self.config.write_text("existing production configuration\n")
        (self.root / "running").write_text(OLD)
        (self.root / "database").write_text("original-database\n")
        self.failed_version = None
        self.release_binary = binary(NEW)
        self.bad_checksum = False
        fixture = self

        class Handler(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                running = (fixture.root / "running").read_text()
                if self.path == "/readyz":
                    self.send_response(503 if running in (fixture.failed_version, "STOPPED") else 200)
                    body = b'{"status":"ok"}'
                elif self.path == "/api/platform/info":
                    self.send_response(200)
                    body = json.dumps({"data": {"version": running}}, separators=(",", ":")).encode()
                else:
                    self.send_response(404)
                    body = b"not found"
                self.end_headers()
                self.wfile.write(body)

            def log_message(self, *_):
                pass

        self.http = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.thread = threading.Thread(target=self.http.serve_forever, daemon=True)
        self.thread.start()
        supervisor = self.root / "supervisorctl"
        supervisor.write_text('''#!/usr/bin/env python3
from pathlib import Path
import subprocess
import sys
root = Path(__file__).parent
assert sys.argv[1:3] == ["-c", str(root / "supervisor.ini")]
action = sys.argv[3]
assert action in ("stop", "start") and sys.argv[4] == "DraARL:DraARL_00"
if action == "stop" and (root / "stop-fails").exists():
    sys.exit(2)
version = subprocess.check_output([str(root / "app/server"), "-v"], text=True).split()[2]
(root / "running").write_text(version if action == "start" else "STOPPED")
with (root / "restarts").open("a") as log:
    log.write(action + " " + version + "\\n")
''')
        supervisor.chmod(0o755)
        url = f"http://127.0.0.1:{self.http.server_port}"
        settings = {
            "APP_DIR": str(self.app), "STATE_DIR": str(self.root / "state"),
            "SUPERVISORCTL": str(supervisor), "SUPERVISOR_CONFIG": str(self.root / "supervisor.ini"),
            "SUPERVISOR_PROGRAM": "DraARL:DraARL_00",
            "HEALTH_URL": url + "/readyz", "VERSION_URL": url + "/api/platform/info",
            "HEALTH_ATTEMPTS": "3", "HEALTH_INTERVAL": "0.01",
            "APP_CONFIG": str(self.config),
        }
        self.deploy_config = self.root / "deploy.conf"
        self.deploy_config.write_text("".join(f"{key}={shlex.quote(value)}\n" for key, value in settings.items()))

    def tearDown(self):
        self.http.shutdown()
        self.http.server_close()
        self.thread.join()
        self.temp.cleanup()

    def package(self):
        output = io.BytesIO()
        with tarfile.open(fileobj=output, mode="w") as archive:
            entry = tarfile.TarInfo("./DraARL-linux-amd64")
            entry.size = len(self.release_binary)
            archive.addfile(entry, io.BytesIO(self.release_binary))
            # Release packages contain a sample config; deployment must leave it alone.
            config = tarfile.TarInfo("./config.yaml")
            config.size = len(b"sample configuration")
            archive.addfile(config, io.BytesIO(b"sample configuration"))
        return gzip.compress(output.getvalue(), mtime=0)

    def deploy(self, version=NEW):
        result = subprocess.run(
            ["bash", str(SCRIPT), version],
            env={**os.environ, "DRAARL_DEPLOY_CONFIG": str(self.deploy_config), "DEPLOY_TEST_ROOT": str(self.root)},
            input=(f'{"0" * 64 if self.bad_checksum else hashlib.sha256(self.package()).hexdigest()}  {ARCHIVE}\n'.encode() + self.package()),
            capture_output=True, timeout=20,
        )
        result.stdout = result.stdout.decode()
        result.stderr = result.stderr.decode()
        self.assertEqual(self.config.read_text(), "existing production configuration\n")
        self.assertEqual(list(self.app.glob(".draarl-deploy.*")), [])
        self.assertFalse((self.root / "state/backups").exists())
        return result

    def test_success_and_repeat_preserve_previous_program_and_configuration(self):
        result = self.deploy()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.server.read_bytes(), binary(NEW))
        self.assertEqual((self.root / "state/server.previous").read_bytes(), binary(OLD))
        self.assertEqual((self.root / "running").read_text(), NEW)
        self.assertEqual((self.root / "state/current-version").read_text().strip(), NEW)
        result = self.deploy()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual((self.root / "restarts").read_text().splitlines(), ["stop " + OLD, "start " + NEW])
        self.assertEqual((self.root / "state/server.previous").read_bytes(), binary(OLD))
        self.assertEqual((self.root / "database").read_text(), "migrated-database\n")

    def test_failed_readiness_after_migration_stays_stopped_for_recovery(self):
        self.failed_version = NEW
        result = self.deploy()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Service remains stopped", result.stderr)
        self.assertEqual(self.server.read_bytes(), binary(NEW))
        self.assertEqual((self.root / "running").read_text(), "STOPPED")
        self.assertTrue((self.root / "state/deployment-failed").exists())
        self.assertNotIn("start " + OLD, (self.root / "restarts").read_text())
        self.assertNotEqual(self.deploy().returncode, 0)

    def test_stop_failure_keeps_old_service_without_running_migration(self):
        (self.root / "stop-fails").touch()
        result = self.deploy()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.server.read_bytes(), binary(OLD))
        self.assertEqual((self.root / "running").read_text(), OLD)
        self.assertFalse((self.root / "migration-ran").exists())
        self.assertFalse((self.root / "state/deployment-failed").exists())

    def test_partial_migration_preserves_previous_program_and_does_not_restart(self):
        (self.root / "migration-fails").touch()
        result = self.deploy()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.server.read_bytes(), binary(OLD))
        self.assertEqual((self.root / "database").read_text(), "partial-migration\n")
        self.assertEqual((self.root / "running").read_text(), "STOPPED")
        self.assertTrue((self.root / "state/deployment-failed").exists())
        self.assertNotIn("start " + OLD, (self.root / "restarts").read_text())

    def test_bad_checksum_does_not_replace_or_restart(self):
        self.bad_checksum = True
        self.assertNotEqual(self.deploy().returncode, 0)
        self.assertEqual(self.server.read_bytes(), binary(OLD))
        self.assertFalse((self.root / "restarts").exists())

    def test_wrong_binary_version_does_not_replace_or_restart(self):
        self.release_binary = binary("v1.0.0")
        self.assertNotEqual(self.deploy().returncode, 0)
        self.assertEqual(self.server.read_bytes(), binary(OLD))
        self.assertFalse((self.root / "restarts").exists())

    def test_binary_without_migration_mode_does_not_stop_service(self):
        self.release_binary = binary(NEW).replace(b"-h) echo '-migrate-only'", b"-h) echo 'old options'")
        self.assertNotEqual(self.deploy().returncode, 0)
        self.assertEqual((self.root / "running").read_text(), OLD)
        self.assertFalse((self.root / "restarts").exists())
        self.assertFalse((self.root / "migration-ran").exists())

    def test_ssh_command_is_a_version_not_a_shell_command(self):
        self.assertEqual(self.deploy("v2.0.1; id").returncode, 2)
        self.assertFalse((self.root / "restarts").exists())


if __name__ == "__main__":
    unittest.main(verbosity=2)
