import os
from pathlib import Path
import subprocess
import tempfile
import unittest


@unittest.skipIf(os.name == "nt", "POSIX installer")
class InstallPermissionsTest(unittest.TestCase):
    def test_generated_launcher_restricts_existing_install_and_new_files(self):
        installer = (Path(__file__).resolve().parent.parent / "install.sh").read_text()
        start = installer.index('LAUNCH_SCRIPT="$INSTALL_DIR/start-bridge.sh"')
        end = installer.index("# ── service helpers", start)
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            root.chmod(0o755)
            bridge = root / "whatsapp-bridge"
            bridge.mkdir()
            env_file = root / "transcription.env"
            env_file.write_text("export TEST_TRANSCRIPTION=1\n")
            env_file.chmod(0o644)
            stub = bridge / "whatsapp-bridge"
            stub.write_text('#!/bin/sh\nprintf test > probe.txt\n')
            stub.chmod(0o700)
            env = dict(os.environ, INSTALL_DIR=tmp, BRIDGE_PORT="8080")
            subprocess.run(["bash", "-c", "success() { :; }\n" + installer[start:end]],
                           env=env, check=True, capture_output=True)
            subprocess.run(["bash", "-c", 'umask 022; exec "$INSTALL_DIR/start-bridge.sh"'],
                           env=env, check=True, capture_output=True)
            self.assertEqual(root.stat().st_mode & 0o777, 0o700)
            self.assertEqual(env_file.stat().st_mode & 0o777, 0o600)
            self.assertEqual((bridge / "probe.txt").stat().st_mode & 0o777, 0o600)

    def test_installer_protects_log_and_existing_env(self):
        installer = (Path(__file__).resolve().parent.parent / "install.sh").read_text()
        header = installer[:installer.index("# WhatsApp MCP")]
        start = installer.index('if [[ -d "$INSTALL_DIR/.git" ]]')
        end = installer.index("# ── build Go bridge", start)
        stubs = "info() { :; }; success() { :; }; git() { :; };\n"
        for existing_log in (False, True):
            with self.subTest(existing_log=existing_log), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                root.chmod(0o755)
                env_file = root / "transcription.env"
                env_file.write_text("export TEST_TRANSCRIPTION=1\n")
                env_file.chmod(0o644)
                bridge_log = root / "bridge.log"
                if existing_log:
                    bridge_log.write_text("old log\n")
                    bridge_log.chmod(0o644)
                subprocess.run(["bash", "-c", header + stubs + installer[start:end]],
                               env=dict(os.environ, INSTALL_DIR=tmp, REPO_URL="unused"),
                               check=True, capture_output=True)
                self.assertEqual(root.stat().st_mode & 0o777, 0o700)
                self.assertEqual(env_file.stat().st_mode & 0o777, 0o600)
                self.assertEqual(bridge_log.stat().st_mode & 0o777, 0o600)
