import os
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import Mock, patch

import whatsapp


class MediaPathTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.base = Path(self.directory.name)
        self.allowed = self.base / "uploads"
        self.allowed.mkdir()
        self.home = self.base / "home"
        self.home.mkdir()
        self.env = patch.dict(os.environ, {
            "HOME": str(self.home), "USERPROFILE": str(self.home),
            "WHATSAPP_MEDIA_DIRS": str(self.allowed),
        })
        self.env.start()
        self.addCleanup(self.env.stop)

    def file(self, path):
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(b"audio")
        return str(path)

    def test_denied_sources_never_reach_conversion_or_http(self):
        secret = self.file(self.base / "secret.wav")
        hidden = self.file(self.home / ".ssh" / "secret.wav")
        sources = [secret, hidden]
        if os.name != "nt":
            alias = self.allowed / "alias.wav"
            alias.symlink_to(secret)
            sources.append(str(alias))
        large = self.allowed / "large.wav"
        with large.open("wb") as f:
            f.truncate(100 * 1024 * 1024 + 1)
        sources.extend([str(large), str(self.allowed)])
        if hasattr(os, "mkfifo"):
            fifo = self.allowed / "fifo.wav"
            os.mkfifo(fifo)
            sources.append(str(fifo))
        for source in sources:
            for send in [whatsapp.send_file, whatsapp.send_audio_message]:
                with self.subTest(source=source, send=send.__name__), \
                        patch("whatsapp.requests.post") as post, \
                        patch("whatsapp.audio.convert_to_opus_ogg_temp") as convert:
                    success, error = send("123", source)
                    self.assertFalse(success)
                    self.assertNotIn(str(self.base), error)
                    post.assert_not_called()
                    convert.assert_not_called()

    def test_converter_reads_checked_snapshot(self):
        source = self.file(self.allowed / "voice.wav")
        converted = self.file(self.allowed / "voice.ogg")
        snapshots = []

        def convert(path):
            self.assertNotEqual(path, source)
            Path(source).write_bytes(b"secret")
            self.assertEqual(Path(path).read_bytes(), b"audio")
            snapshots.append(path)
            return converted

        response = Mock(status_code=200)
        response.json.return_value = {"success": True, "message": "sent"}
        with patch("whatsapp.audio.convert_to_opus_ogg_temp", side_effect=convert), \
                patch("whatsapp.requests.post", return_value=response):
            self.assertTrue(whatsapp.send_audio_message("123", source)[0])
        self.assertTrue(snapshots)
        self.assertFalse(Path(snapshots[0]).exists())

    def test_allowed_file_and_ogg_keep_original_paths(self):
        response = Mock(status_code=200)
        response.json.return_value = {"success": True, "message": "sent"}
        for send, name in [(whatsapp.send_file, "report.pdf"),
                           (whatsapp.send_audio_message, "voice.ogg")]:
            path = self.file(self.allowed / name)
            with patch("whatsapp.requests.post", return_value=response) as post:
                self.assertTrue(send("123", path)[0])
                self.assertEqual(post.call_args.kwargs["json"]["media_path"], path)

    def test_defaults_allow_downloads_and_temp_but_exclude_credentials(self):
        store = self.home / ".local" / "whatsapp-mcp" / "store"
        temp = self.base / "temp"
        temp.mkdir()
        response = Mock(status_code=200)
        response.json.return_value = {"success": True}
        with patch.dict(os.environ, {"WHATSAPP_MEDIA_DIRS": ""}), \
                patch("media_paths.STORE_DIR", store), \
                patch("media_paths.tempfile.gettempdir", return_value=str(temp)), \
                patch("whatsapp.requests.post", return_value=response) as post:
            for path in [self.home / "Documents" / "report.pdf", temp / "tts.ogg",
                         store / "chat" / "message.ogg"]:
                self.assertTrue(whatsapp.send_file("123", self.file(path))[0])
            for path in [self.home / ".ssh" / "key", self.home / ".aws" / "credentials",
                         self.home / "Library" / "Messages" / "chat.db",
                         self.home / "AppData" / "Roaming" / "cookies",
                         store / "whatsapp.db", store / "messages.db-wal",
                         self.base / "outside", self.home / ".local" / "other"]:
                with self.subTest(path=path):
                    post.reset_mock()
                    success, error = whatsapp.send_file("123", self.file(path))
                    self.assertFalse(success)
                    self.assertNotIn(str(self.base), error)
                    post.assert_not_called()

    def test_custom_roots_do_not_override_exclusions(self):
        store = self.home / ".local" / "whatsapp-mcp" / "store"
        with patch.dict(os.environ, {"WHATSAPP_MEDIA_DIRS": str(self.home)}), \
                patch("media_paths.STORE_DIR", store), \
                patch("whatsapp.requests.post") as post:
            for path in [self.home / ".ssh" / "key", store / "whatsapp.db"]:
                self.assertFalse(whatsapp.send_file("123", self.file(path))[0])
            post.assert_not_called()

    def test_windows_handle_path_normalizes_drive_and_unc_paths(self):
        from media_paths import _windows_handle_path

        for native, expected in [("\\\\?\\C:\\uploads\\voice.wav", "C:\\uploads\\voice.wav"),
                                 ("\\\\?\\UNC\\host\\share\\voice.wav", "\\\\host\\share\\voice.wav")]:
            def final_path(handle, buffer, size, flags):
                if buffer is None:
                    return len(native) + 1
                buffer.value = native
                return len(native)

            api = Mock(side_effect=final_path)
            with patch.dict(sys.modules, {"msvcrt": Mock()}), \
                    patch("ctypes.WinDLL", create=True) as dll:
                dll.return_value.GetFinalPathNameByHandleW = api
                self.assertEqual(_windows_handle_path(42), expected)

    def test_conversion_failure_cleans_snapshot_and_hides_paths(self):
        source = self.file(self.allowed / "voice.wav")
        snapshots = []

        def convert(path):
            snapshots.append(path)
            raise RuntimeError(f"ffmpeg could not read {path}")

        with patch("whatsapp.audio.convert_to_opus_ogg_temp", side_effect=convert), \
                patch("whatsapp.requests.post") as post:
            success, error = whatsapp.send_audio_message("123", source)
            self.assertFalse(success)
            self.assertTrue(snapshots)
            self.assertNotIn(snapshots[0], error)
            self.assertFalse(Path(snapshots[0]).exists())
            post.assert_not_called()


if __name__ == "__main__":
    unittest.main()
