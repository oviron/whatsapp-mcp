"""download_media is returned to MCP clients (including over the remote HTTP
transport), so its failure text must be a short, status-based reason and never
the bridge's raw body, which can carry server paths.

Run: python -m unittest test_download_media_errors -v"""

import unittest
import unittest.mock

import requests

import whatsapp

LEAK = "mkdir /srv/secret/store: permission denied"


class FakeResponse:
    def __init__(self, status_code, json_body=None, text=""):
        self.status_code = status_code
        self._json = json_body
        self.text = text
        self.headers = {}

    def json(self):
        if self._json is None:
            raise ValueError("not json")
        return self._json


def _download(outcome):
    if isinstance(outcome, Exception):
        patch = unittest.mock.patch.object(whatsapp.requests, "post", side_effect=outcome)
    else:
        patch = unittest.mock.patch.object(whatsapp.requests, "post", return_value=outcome)
    with patch:
        return whatsapp.download_media("MSG", "chat@s.whatsapp.net")


def _error(outcome):
    try:
        _download(outcome)
    except whatsapp.BridgeError as error:
        return str(error)
    raise AssertionError("download_media did not fail")


class DownloadMediaErrorTest(unittest.TestCase):
    def test_raw_bridge_text_never_reaches_the_client(self):
        outcomes = [
            FakeResponse(500, {"success": False, "message": LEAK}),
            FakeResponse(500, text=LEAK),
            FakeResponse(200, {"success": False, "message": LEAK}),
            FakeResponse(200, text=LEAK),
            FakeResponse(401, text=LEAK),
            FakeResponse(404, text=LEAK),
            FakeResponse(500, {"success": False, "message": LEAK, "reason": "media_gone"}),
            requests.ConnectionError(LEAK),
        ]
        for outcome in outcomes:
            with self.subTest(outcome=repr(getattr(outcome, "status_code", outcome))):
                message = _error(outcome)
                self.assertTrue(message)
                self.assertNotIn("mkdir", message)
                self.assertNotIn("/srv/secret", message)

    def test_messages_are_status_based(self):
        self.assertIn("401", _error(FakeResponse(401, text="x")))
        self.assertIn("404", _error(FakeResponse(404, text="x")))
        self.assertIn("unavailable", _error(requests.ConnectionError("x")))
        self.assertIn("no longer available",
                      _error(FakeResponse(500, {"message": "x", "reason": "media_gone"})))

    def test_success_still_returns_the_path(self):
        self.assertEqual(_download(FakeResponse(200, {"success": True, "path": "/tmp/x.ogg"})), "/tmp/x.ogg")


if __name__ == "__main__":
    unittest.main()
