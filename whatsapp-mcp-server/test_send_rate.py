import asyncio
import unittest
from unittest.mock import Mock, patch

import whatsapp
from test_mcp_modes import load_main


class SendRateTests(unittest.TestCase):
    def response(self, retry="30"):
        response = Mock(status_code=429, headers={"Retry-After": retry})
        response.text = "private bridge detail"
        response.json.return_value = {"message": response.text}
        return response

    def test_direct_sends_report_safe_rate_error_without_retry(self):
        for call in (
            lambda: whatsapp.send_message("peer", "hello"),
            lambda: whatsapp.send_file("peer", "test.ogg"),
            lambda: whatsapp.send_audio_message("peer", "test.ogg"),
            lambda: whatsapp.create_group("test", ["peer"]),
        ):
            with self.subTest(call=call), patch.object(whatsapp, "open_media"), patch.object(
                whatsapp.requests, "post", return_value=self.response()
            ) as post:
                result = call()
                self.assertFalse(result[0])
                self.assertIn("rate limit", result[1].lower())
                self.assertIn("30", result[1])
                self.assertNotIn("private bridge detail", result[1])
                post.assert_called_once()

    def test_shared_actions_and_drafts_report_rate_error_without_retry(self):
        for call in (
            lambda: whatsapp.send_contact("peer", "name", "number"),
            lambda: whatsapp.create_poll("peer", "question", ["yes", "no"]),
            lambda: whatsapp.vote_poll("peer", "id", ["yes"]),
            lambda: whatsapp.react_to_message("peer", "id", ""),
            lambda: whatsapp.edit_message("peer", "id", "new text"),
            lambda: whatsapp.delete_message("peer", "id"),
            lambda: whatsapp.matter_call("/drafts/approve", {"draft_id": 1}),
        ):
            with self.subTest(call=call), patch.object(
                whatsapp.requests, "request", return_value=self.response()
            ) as request:
                result = call()
                message = result["error"] if isinstance(result, dict) else result[1]
                self.assertIn("rate limit", message.lower())
                self.assertIn("30", message)
                self.assertNotIn("private bridge detail", message)
                request.assert_called_once()

    def test_untrusted_retry_header_is_not_echoed(self):
        with patch.object(whatsapp.requests, "post", return_value=self.response("private bridge detail")):
            _, message = whatsapp.send_message("peer", "hello")
        self.assertIn("rate limit", message.lower())
        self.assertNotIn("private bridge detail", message)

    def test_rate_error_reaches_mcp_tool(self):
        mod = load_main()
        with patch.object(whatsapp.requests, "post", return_value=self.response()) as post:
            result = asyncio.run(mod.mcp.call_tool("send_message", {"recipient": "peer", "message": "hello"}))
        text = " ".join(item.text for item in result)
        self.assertIn('"success": false', text)
        self.assertIn("rate limit", text.lower())
        self.assertIn("30", text)
        self.assertNotIn("private bridge detail", text)
        post.assert_called_once()


if __name__ == "__main__":
    unittest.main()
