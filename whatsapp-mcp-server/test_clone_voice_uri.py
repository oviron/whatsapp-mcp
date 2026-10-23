import os
import sqlite3
import tempfile
import unittest
from pathlib import Path

import clone_voice


def _store(db):
    c = sqlite3.connect(db)
    c.execute("CREATE TABLE messages (id, chat_jid, content, filename, timestamp, is_from_me, media_type)")
    c.execute("INSERT INTO messages VALUES ('mine', 'a@s.whatsapp.net', 'ola', 'x.ogg', '2026-01-01', 1, 'audio')")
    c.commit()
    c.close()


class StorePathWithURICharactersTest(unittest.TestCase):
    def test_reads_the_named_database(self):
        for name in ("a#b", "a?b", "a%41b", "a b", "a#b?c%41 d"):
            with self.subTest(name), tempfile.TemporaryDirectory() as d:
                db = Path(d) / name / "messages.db"
                db.parent.mkdir()
                _store(db)
                self.assertEqual([r[0] for r in clone_voice.rows(db, None, None)], ["mine"])

    def test_relative_path(self):
        with tempfile.TemporaryDirectory() as d:
            (Path(d) / "s#1").mkdir()
            _store(Path(d) / "s#1" / "messages.db")
            cwd = os.getcwd()
            os.chdir(d)
            self.addCleanup(os.chdir, cwd)
            self.assertEqual([r[0] for r in clone_voice.rows(Path("s#1/messages.db"), None, None)], ["mine"])


if __name__ == "__main__":
    unittest.main()
