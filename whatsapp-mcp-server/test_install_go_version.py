import os
from pathlib import Path
import subprocess
import unittest


@unittest.skipIf(os.name == "nt", "POSIX installer")
class InstallGoVersionTest(unittest.TestCase):
    def test_go_version_gate(self):
        installer = (Path(__file__).resolve().parent.parent / "install.sh").read_text()
        start = installer.index("GO_VERSION=$(go version")
        end = installer.index('need python3', start)
        for version, accepted in (("1.25.9", False), ("1.26.0", True), ("1.27.3", True), ("2.0.0", True)):
            with self.subTest(version=version):
                script = ('set -eu\ngo() { echo "go version go$TEST_GO_VERSION test/arch"; }\n'
                          'die() { echo "$*" >&2; exit 1; }\nsuccess() { :; }\n'
                          + installer[start:end])
                result = subprocess.run(["bash", "-c", script], capture_output=True,
                                        text=True, env=dict(os.environ, TEST_GO_VERSION=version))
                self.assertEqual(result.returncode, 0 if accepted else 1)
                if not accepted:
                    self.assertIn("Go 1.26+ required", result.stderr)
