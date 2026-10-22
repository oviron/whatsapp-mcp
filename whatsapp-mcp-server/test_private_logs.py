import contextlib
import io
import os
import tempfile
import subprocess
import unittest
from unittest.mock import Mock, patch

import recover_audios
import transcribe
import tts
import whatsapp


class PrivateLogsTest(unittest.TestCase):
    def test_transcription_content_is_opt_in(self):
        for flag in ("", "0", "false", "1", "true", "yes", "on", " TRUE "):
            with self.subTest(flag=flag), tempfile.NamedTemporaryFile() as audio:
                output = io.StringIO()
                with patch.dict(os.environ, {"WHATSAPP_LOG_CONTENT": flag}), \
                     patch("sys.argv", ["transcribe.py"]), \
                     patch.object(transcribe, "engine_ready", return_value=(True, "local")), \
                     patch.object(transcribe.sqlite3, "connect"), \
                     patch.object(transcribe, "pending_audios", return_value=[("msg", "chat", None, 1, "2026-10-22")]), \
                     patch.object(transcribe, "cached_audio", return_value=audio.name), \
                     patch.object(transcribe, "valid_audio_file", return_value=True), \
                     patch.object(transcribe, "transcribe", return_value="PRIVATE TRANSCRIPT"), \
                     patch.object(transcribe, "write_content"), contextlib.redirect_stdout(output):
                    transcribe.main()
                self.assertEqual("PRIVATE TRANSCRIPT" in output.getvalue(), flag.strip().lower() in ("1", "true", "yes", "on"))
                self.assertIn("18", output.getvalue())

    def test_download_logs_keep_reason_but_hide_urls(self):
        secret = "connection refused: https://media.invalid/SECRET"
        for flag in ("", "0", "false", "1", "true", "yes", "on", " TRUE "):
            for status in (200, 500):
                response = Mock(status_code=status, text=secret)
                response.json.return_value = {"success": False, "message": secret}
                with self.subTest(flag=flag, status=status), \
                     patch.dict(os.environ, {"WHATSAPP_LOG_CONTENT": flag}), \
                     patch.object(whatsapp.requests, "post", return_value=response), \
                     self.assertLogs(whatsapp.logger, level="WARNING") as logs:
                    self.assertIsNone(whatsapp.download_media("msg", "chat"))
                self.assertNotIn(secret, " ".join(logs.output))
                self.assertIn("connection refused", " ".join(logs.output))

    def test_tts_body_is_hidden_but_cli_stderr_is_logged(self):
        secret = "PRIVATE MESSAGE MediaKey=PRIVATE_KEY https://media.invalid/SECRET"
        for flag in ("", "0", "false", "1", "true", "yes", "on", " TRUE "):
            with self.subTest(flag=flag), tempfile.TemporaryDirectory() as tmp, \
                 patch.dict(os.environ, {"WHATSAPP_LOG_CONTENT": flag}), \
                 patch.object(tts.requests, "post", return_value=Mock(status_code=500, text=secret)), \
                 self.assertLogs(tts.log, level="ERROR") as logs:
                with self.assertRaises(tts.TTSError):
                    tts._synthesize_api("PRIVATE MESSAGE", tmp)
            self.assertNotIn(secret, " ".join(logs.output))
            self.assertNotIn("PRIVATE_KEY", " ".join(logs.output))
            with tempfile.TemporaryDirectory() as tmp, \
                 patch.object(tts.subprocess, "run", return_value=Mock(returncode=1, stderr=("x" * 400 + secret).encode())), \
                 self.assertLogs(tts.log, level="ERROR") as logs:
                with self.assertRaises(tts.TTSError):
                    tts._synthesize_local("PRIVATE MESSAGE", tmp)
            self.assertIn(secret, " ".join(logs.output))
            self.assertNotIn("x" * 301, " ".join(logs.output))

    def test_recovery_transcript_is_opt_in(self):
        for flag in ("", "0", "false", "1", "true", "yes", "on", " TRUE "):
            with self.subTest(flag=flag), tempfile.NamedTemporaryFile() as bridge_log:
                output = io.StringIO()
                with patch.dict(os.environ, {"WHATSAPP_LOG_CONTENT": flag}), \
                     patch("sys.argv", ["recover_audios.py", "--quiet", "0"]), \
                     patch.object(recover_audios, "BRIDGE_LOG", bridge_log.name), \
                     patch.object(recover_audios, "engine_ready", return_value=(True, "local")), \
                     patch.object(recover_audios.sqlite3, "connect"), \
                     patch.object(recover_audios, "unavailable_audios", return_value=[("msg", "chat")]), \
                     patch.object(recover_audios, "request_retry", return_value=True), \
                     patch.object(recover_audios.time, "sleep"), \
                     patch.object(recover_audios, "scan_log_since", return_value=({"msg"}, set(), set(), set(), 0)), \
                     patch.object(recover_audios, "recovered_file", return_value="audio.ogg"), \
                     patch.object(recover_audios, "transcribe", return_value="PRIVATE TRANSCRIPT"), \
                     patch.object(recover_audios, "write_content"), contextlib.redirect_stdout(output):
                    recover_audios.main()
                self.assertEqual("PRIVATE TRANSCRIPT" in output.getvalue(), flag.strip().lower() in ("1", "true", "yes", "on"))

    def test_transcription_errors_preserve_diagnostics(self):
        reason = "model file not found"
        errors = [RuntimeError(reason), subprocess.CalledProcessError(1, ["whisper"], stderr="x" * 400 + reason),
                  transcribe.FatalTranscriptionError(reason)]
        for flag in ("", "0", "false", "1", "true", "yes", "on", " TRUE "):
            for error in errors:
                with self.subTest(flag=flag, error=type(error).__name__), tempfile.NamedTemporaryFile() as audio:
                    output = io.StringIO()
                    with patch.dict(os.environ, {"WHATSAPP_LOG_CONTENT": flag}), \
                         patch("sys.argv", ["transcribe.py"]), \
                         patch.object(transcribe, "engine_ready", return_value=(True, "local")), \
                         patch.object(transcribe.sqlite3, "connect"), \
                         patch.object(transcribe, "pending_audios", return_value=[("msg", "chat", None, 1, "2026-10-22")]), \
                         patch.object(transcribe, "cached_audio", return_value=audio.name), \
                         patch.object(transcribe, "valid_audio_file", return_value=True), \
                         patch.object(transcribe, "transcribe", side_effect=error), contextlib.redirect_stdout(output):
                        transcribe.main()
                    self.assertIn(reason, output.getvalue())
                    self.assertNotIn("x" * 301, output.getvalue())

    def test_download_failure_keeps_reason_and_redacts_signed_url(self):
        output = io.StringIO()
        with patch("sys.argv", ["transcribe.py"]), \
             patch.object(transcribe, "engine_ready", return_value=(True, "local")), \
             patch.object(transcribe.sqlite3, "connect"), \
             patch.object(transcribe, "pending_audios", return_value=[("msg", "chat", None, 1, "2026-10-22")]), \
             patch.object(transcribe, "cached_audio", return_value=None), \
             patch.object(transcribe, "download", return_value=(None, "Get https://cdn.invalid/file?sig=SECRET: connection refused", None)), \
             contextlib.redirect_stdout(output):
            transcribe.main()
        self.assertIn("connection refused", output.getvalue())
        self.assertNotIn("SECRET", output.getvalue())
        self.assertNotIn("cdn.invalid", output.getvalue())
