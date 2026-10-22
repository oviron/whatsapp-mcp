# Security policy

## Reporting a vulnerability

Do not open a public issue. Use GitHub's private vulnerability reporting:
**Security tab > Report a vulnerability** on this repository. Include steps to reproduce and the affected version or commit.

## Supported versions

Only the latest `main`. Fixes land there; there are no backport branches.

## Security model

- The bridge is a local service that holds your full WhatsApp session and message history (SQLite on disk). Treat its data directory like a password.
- The REST API binds to `127.0.0.1` by default.
- Binding beyond loopback (`BIND_ADDR`) requires `API_AUTH_TOKEN`; the bridge refuses to start without it. A VPN or private network is not a substitute for the token.
- Downloaded media filenames are validated: components with separators or `..` are rejected, not silently sanitized.
- Outbound media is limited to `WHATSAPP_MEDIA_DIRS` (default: `~/Downloads`, OS temp and downloaded media in `store/<chat>/`), checked after symlink resolution and read from a checked descriptor with a 100 MiB limit. Documents, projects and other home files require an explicit root; allowing the whole home requires explicitly listing it. Comparisons use filesystem identity to handle case aliases on macOS and Windows. Hidden top-level home entries, `~/Library` and `~/AppData` are excluded even with custom roots, except for OS temp and `store/<chat>/` inside them. Files directly in `store/` are always excluded. A nonempty `WHATSAPP_MEDIA_DIRS` replaces the default roots, so include OS temp for TTS/audio conversion and `store/` for forwarding as needed.
- Audio transcription is opt-in; nothing is sent to a third-party speech API unless you configure it.
- This is an unofficial client (whatsmeow, WhatsApp Web multi-device protocol). WhatsApp may rate-limit or ban accounts that automate; use at your own risk.
- Prompt injection: the MCP server is subject to the [lethal trifecta](https://simonwillison.net/2025/Jun/16/the-lethal-trifecta/). Any incoming message is untrusted text an LLM may read, and the LLM can send messages and read private data, so a crafted message could exfiltrate data. Review tool calls that send or forward content.
- Secrets (`transcription.env`, tokens, session databases) are gitignored and must never be committed.
