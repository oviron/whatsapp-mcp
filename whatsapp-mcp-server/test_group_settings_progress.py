import asyncio
import json
import unittest
from unittest.mock import Mock, patch

import whatsapp
from test_mcp_modes import load_main


class GroupSettingsProgressTests(unittest.TestCase):
    def response(self, status, applied):
        response = Mock(status_code=status, headers={"Retry-After": "30"})
        response.text = "private bridge detail"
        response.json.return_value = {
            "success": status == 200,
            "message": response.text,
            "error": response.text if status != 200 else None,
            "applied": applied,
        }
        return response

    def test_partial_rate_limit_and_retry_remaining(self):
        with patch.object(whatsapp.requests, "post", side_effect=[
            self.response(429, ["name"]), self.response(200, ["topic"]),
        ]) as request:
            ok, message = whatsapp.update_group_settings("123@g.us", name="test", topic="topic")
            self.assertFalse(ok)
            self.assertIn("rate limit", message.lower())
            self.assertIn("30", message)
            self.assertIn("applied fields: name", message.lower())
            self.assertNotIn("private bridge detail", message)
            self.assertEqual(request.call_count, 1)
            ok, message = whatsapp.update_group_settings("123@g.us", topic="topic")
            self.assertTrue(ok)
            self.assertEqual(request.call_count, 2)
            self.assertIn("applied fields: topic", message.lower())
            self.assertNotIn("private bridge detail", message)
            self.assertEqual(request.call_args.kwargs["json"], {"group_jid": "123@g.us", "topic": "topic"})

    def test_partial_server_failure_and_mcp_progress_are_safe(self):
        mod = load_main()
        with patch.object(whatsapp.requests, "post", return_value=self.response(500, ["name"])):
            result = asyncio.run(mod.mcp.call_tool("update_group_settings", {
                "group_jid": "123@g.us", "name": "test", "topic": "topic",
            }))
        text = " ".join(item.text for item in result)
        self.assertIn('"success": false', text)
        self.assertIn("500", text)
        self.assertIn("applied fields: name", text.lower())
        self.assertNotIn("private bridge detail", text)

    def test_older_bridge_without_applied_omits_progress(self):
        for status in (200, 429, 500):
            response = self.response(status, [])
            del response.json.return_value["applied"]
            if status == 200:
                response.json.return_value = {"success": True, "message": "updated: name"}
            with self.subTest(status=status), patch.object(
                whatsapp.requests, "post", return_value=response
            ) as post:
                ok, message = whatsapp.update_group_settings("123@g.us", name="test")
            self.assertEqual(ok, status == 200)
            self.assertNotIn("applied fields", message.lower())
            self.assertNotIn("private bridge detail", message)
            post.assert_called_once()

    def test_partial_rate_error_reaches_mcp_tool_without_retry(self):
        mod = load_main()
        with patch.object(whatsapp.requests, "post", return_value=self.response(429, ["name"])) as post:
            result = asyncio.run(mod.mcp.call_tool("update_group_settings", {
                "group_jid": "123@g.us", "name": "test", "topic": "topic",
            }))
        text = " ".join(item.text for item in result)
        self.assertIn('"success": false', text)
        self.assertIn("30 seconds", text)
        self.assertIn("applied fields: name", text.lower())
        self.assertNotIn("private bridge detail", text)
        post.assert_called_once()

    def test_applied_fields_are_allowlisted(self):
        for status in (200, 429, 500):
            for applied in (["name", "private bridge detail", {"topic": "secret"}, "name", "locked"],
                            [], "private bridge detail", None, {"name": "private bridge detail"}):
                with self.subTest(status=status, applied=applied), patch.object(
                    whatsapp.requests, "post", return_value=self.response(status, applied)
                ):
                    ok, message = whatsapp.update_group_settings("123@g.us", name="test", locked=True)
                    self.assertEqual(ok, status == 200)
                    self.assertNotIn("private bridge detail", message)
                    self.assertIn("applied fields: name, locked" if applied and isinstance(applied, list) else "applied fields: none", message.lower())

    def test_malformed_responses_and_transport_errors_are_safe(self):
        for result in ([], None, "private bridge detail"):
            response = self.response(200, [])
            response.json.return_value = result
            with self.subTest(result=result), patch.object(whatsapp.requests, "post", return_value=response):
                self.assertEqual(whatsapp.update_group_settings("123@g.us", topic="topic"), (False, "Bridge returned an invalid response"))
        response.json.side_effect = json.JSONDecodeError("private bridge detail", "", 0)
        with patch.object(whatsapp.requests, "post", return_value=response):
            self.assertEqual(whatsapp.update_group_settings("123@g.us", topic="topic"), (False, "Bridge returned invalid JSON"))
        with patch.object(whatsapp.requests, "post", side_effect=whatsapp.requests.RequestException("private bridge detail")):
            ok, message = whatsapp.update_group_settings("123@g.us", topic="topic")
        self.assertFalse(ok)
        self.assertNotIn("private bridge detail", message)


if __name__ == "__main__":
    unittest.main()
