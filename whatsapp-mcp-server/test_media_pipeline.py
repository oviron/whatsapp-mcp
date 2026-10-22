import hashlib
import os
from pathlib import Path
import sqlite3
import tempfile
import unittest
from unittest.mock import patch

import transcribe
import recover_audios


class MediaPipelineCacheTest(unittest.TestCase):
    def test_temporary_files_are_not_audio_cache(self):
        with tempfile.TemporaryDirectory() as root:
            db = Path(root) / "messages.db"
            chat_dir = Path(root) / "chat"
            chat_dir.mkdir()
            data = b"complete audio"
            sha = hashlib.sha256(data).hexdigest()
            temporary = chat_dir / "msg_clip.ogg.123.tmp"
            temporary.write_bytes(data)
            with patch.object(transcribe, "DB_PATH", str(db)):
                self.assertIsNone(transcribe.cached_audio("msg", "chat", sha, len(data)))
                self.assertFalse(transcribe.valid_audio_file(str(temporary), sha, len(data)))
                self.assertIsNone(transcribe.cached_audio("msg", "chat", "", 0))

    def test_sha_and_length_must_both_match(self):
        with tempfile.TemporaryDirectory() as root:
            path = Path(root) / "audio.ogg"
            path.write_bytes(b"audio")
            sha = hashlib.sha256(b"audio").hexdigest()
            self.assertFalse(transcribe.valid_audio_file(str(path), sha, 99))
            self.assertTrue(transcribe.valid_audio_file(str(path), sha, 5))

    def test_recovery_uses_completed_cache_only(self):
        with tempfile.TemporaryDirectory() as root:
            db = Path(root) / "messages.db"
            conn = sqlite3.connect(db)
            conn.execute("CREATE TABLE messages (id TEXT, chat_jid TEXT, file_sha256 BLOB, file_length INTEGER)")
            conn.execute("INSERT INTO messages VALUES ('msg', 'chat', NULL, 0)")
            conn.commit()
            conn.close()
            chat_dir = Path(root) / "chat"
            chat_dir.mkdir()
            temporary = chat_dir / "msg_clip.ogg.123.tmp"
            temporary.write_bytes(b"audio")
            with patch.object(transcribe, "DB_PATH", str(db)), patch.object(recover_audios, "DB_PATH", str(db)):
                self.assertIsNone(recover_audios.recovered_file("chat", "msg"))
                final = chat_dir / "msg_clip.ogg"
                os.rename(temporary, final)
                self.assertEqual(recover_audios.recovered_file("chat", "msg"), str(final))
