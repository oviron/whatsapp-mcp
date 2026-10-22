import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from media_paths import MediaPathError, open_media


class MediaRootsRegressionTests(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.base = Path(directory.name)
        self.home = self.base / "home"
        self.home.mkdir()
        self.store = self.home / "Bridge" / "store"
        self.temp = self.base / "temp"
        self.temp.mkdir()
        for mock in [patch.dict(os.environ, {
                "HOME": str(self.home), "USERPROFILE": str(self.home),
                "WHATSAPP_MEDIA_DIRS": ""}),
                patch("media_paths.STORE_DIR", self.store),
                patch("media_paths.tempfile.gettempdir", return_value=str(self.temp))]:
            mock.start()
            self.addCleanup(mock.stop)

    def file(self, path):
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(b"media")
        return path

    def allowed(self, path):
        with open_media(path) as source:
            self.assertEqual(source.read(), b"media")

    def denied(self, path):
        with self.assertRaises(MediaPathError):
            with open_media(path):
                self.fail("opened a denied source")

    def test_narrow_defaults_and_explicit_home(self):
        for name in ["Documents/report.pdf", "projects/source.txt", "file.txt"]:
            path = self.file(self.home / name)
            self.denied(path)
            with patch.dict(os.environ, {"WHATSAPP_MEDIA_DIRS": str(self.home)}):
                self.allowed(path)
        for path in [self.home / "Downloads" / "file.pdf", self.temp / "tts.ogg",
                     self.store / "chat" / "voice.ogg"]:
            self.allowed(self.file(path))

    def test_temp_inside_excluded_directory(self):
        for zone in ["AppData", "Library", ".cache"]:
            temp = self.home / zone / "Local" / "Temp"
            path = self.file(temp / "tts.ogg")
            with patch("media_paths.tempfile.gettempdir", return_value=str(temp)):
                for roots in ["", str(temp)]:
                    with self.subTest(zone=zone, roots=roots), \
                            patch.dict(os.environ, {"WHATSAPP_MEDIA_DIRS": roots}):
                        self.allowed(path)
                with patch.dict(os.environ, {"WHATSAPP_MEDIA_DIRS": str(self.home)}):
                    self.denied(self.file(self.home / zone / "secret"))

    def test_case_aliases(self):
        home_alias = self.base / "HOME"
        if not home_alias.exists() or not home_alias.samefile(self.home):
            self.skipTest("case-sensitive filesystem")
        download = self.file(self.store / "chat" / "voice.ogg")
        for path in [self.home / "Library" / "secret", self.home / "AppData" / "secret",
                     self.home / ".ssh" / "key", self.store / "whatsapp.db"]:
            self.file(path)
        for root in [self.home, home_alias]:
            with patch.dict(os.environ, {"WHATSAPP_MEDIA_DIRS": str(root)}):
                for path in [self.home / "library" / "secret", self.home / "appdata" / "secret",
                             home_alias / ".ssh" / "key", home_alias / "bridge" / "STORE" / "whatsapp.db"]:
                    with self.subTest(root=root, path=path):
                        self.denied(path)
                self.allowed(download)
        for root in [home_alias / "bridge" / "STORE", self.base / "UPLOADS"]:
            path = download if root.name == "STORE" else self.file(self.base / "Uploads" / "file.pdf")
            with patch.dict(os.environ, {"WHATSAPP_MEDIA_DIRS": str(root)}):
                self.allowed(path)

    def test_distinct_case_directories(self):
        root = self.base / "Uploads"
        self.file(root / "file.pdf")
        alias = self.base / "uploads"
        if alias.exists():
            self.skipTest("case-insensitive filesystem")
        with patch.dict(os.environ, {"WHATSAPP_MEDIA_DIRS": str(root)}):
            self.denied(self.file(alias / "secret"))
