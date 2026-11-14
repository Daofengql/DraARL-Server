"""Exercise the actual deployment script with local HTTP and Supervisor boundaries."""
import gzip
import hashlib
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
NEW = "v2.0.1-alpha12"
ARCHIVE = "DraARL-linux-amd64.tar.gz"


def binary(version):
    return f'#!/bin/sh\nprintf "DraARL version {version} (build time: test)\\n"\n'.encode()


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
        self.failed_version = None
        self.release_binary = binary(NEW)
        self.bad_checksum = False
        fixture = self

        class Handler(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                running = (fixture.root / "running").read_text()
                if self.path == "/readyz":
                    self.send_response(503 if running == fixture.failed_version else 200)
                    body = b'{"status":"ok"}'
                elif self.path == "/api/platform/info":
                    self.send_response(200)
                    body = json.dumps({"data": {"version": running}}, separators=(",", ":")).encode()
                elif self.path == f"/releases/{NEW}/{ARCHIVE}":
                    self.send_response(200)
                    body = fixture.package()
                elif self.path == f"/releases/{NEW}/{ARCHIVE}.sha256":
                    self.send_response(200)
                    digest = "0" * 64 if fixture.bad_checksum else hashlib.sha256(fixture.package()).hexdigest()
                    body = f"{digest}  {ARCHIVE}\n".encode()
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
assert sys.argv[1:] == ["-c", str(root / "supervisor.ini"), "restart", "DraARL:DraARL_00"]
version = subprocess.check_output([str(root / "app/server"), "-v"], text=True).split()[2]
(root / "running").write_text(version)
with (root / "restarts").open("a") as log:
    log.write(version + "\\n")
''')
        supervisor.chmod(0o755)
        url = f"http://127.0.0.1:{self.http.server_port}"
        settings = {
            "APP_DIR": str(self.app), "STATE_DIR": str(self.root / "state"),
            "SUPERVISORCTL": str(supervisor), "SUPERVISOR_CONFIG": str(self.root / "supervisor.ini"),
            "SUPERVISOR_PROGRAM": "DraARL:DraARL_00", "RELEASE_BASE_URL": url + "/releases",
            "HEALTH_URL": url + "/readyz", "VERSION_URL": url + "/api/platform/info",
            "HEALTH_ATTEMPTS": "3", "HEALTH_INTERVAL": "0.01",
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
            env={**os.environ, "DRAARL_DEPLOY_CONFIG": str(self.deploy_config)},
            capture_output=True, text=True, timeout=20,
        )
        self.assertEqual(self.config.read_text(), "existing production configuration\n")
        self.assertEqual(list(self.app.glob(".draarl-deploy.*")), [])
        return result

    def test_success_and_repeat_preserve_backup_and_configuration(self):
        result = self.deploy()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.server.read_bytes(), binary(NEW))
        self.assertEqual((self.root / "state/server.previous").read_bytes(), binary(OLD))
        self.assertEqual((self.root / "running").read_text(), NEW)
        self.assertEqual((self.root / "state/current-version").read_text().strip(), NEW)
        result = self.deploy()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual((self.root / "restarts").read_text().splitlines(), [NEW])
        self.assertEqual((self.root / "state/server.previous").read_bytes(), binary(OLD))

    def test_failed_readiness_restores_previous_running_binary(self):
        self.failed_version = NEW
        result = self.deploy()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Restored " + OLD, result.stderr)
        self.assertEqual(self.server.read_bytes(), binary(OLD))
        self.assertEqual((self.root / "running").read_text(), OLD)
        self.assertEqual((self.root / "restarts").read_text().splitlines(), [NEW, OLD])

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

    def test_ssh_command_is_a_version_not_a_shell_command(self):
        self.assertEqual(self.deploy("v2.0.1; id").returncode, 2)
        self.assertFalse((self.root / "restarts").exists())


if __name__ == "__main__":
    unittest.main(verbosity=2)
