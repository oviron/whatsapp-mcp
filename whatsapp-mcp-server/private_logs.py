import re


def redact_urls(value):
    return re.sub(r"https?://\S+", "<url>", str(value))
