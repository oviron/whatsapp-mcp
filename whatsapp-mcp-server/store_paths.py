import os
from pathlib import Path

BRIDGE_DIR = Path(__file__).resolve().parent.parent / "whatsapp-bridge"
STORE_DIR = Path(os.path.abspath(BRIDGE_DIR / (os.environ.get("WHATSAPP_STORE_DIR") or "store")))
