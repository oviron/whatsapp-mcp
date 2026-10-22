package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
)

func assertPrivateMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != want {
		t.Errorf("%s mode=%o want=%o", path, info.Mode().Perm(), want)
	}
}

func TestNewStorePermissions(t *testing.T) {
	setupChatStore(t)
	assertPrivateMode(t, "store", 0700)
	assertPrivateMode(t, "store/messages.db", 0600)
}

func TestExistingStorePermissions(t *testing.T) {
	s := setupChatStore(t)
	s.Close()
	if err := os.Chmod("store", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod("store/messages.db", 0644); err != nil {
		t.Fatal(err)
	}
	// Sidecars of the session DB are not opened by NewMessageStore.
	for _, name := range []string{"whatsapp.db", "whatsapp.db-wal", "whatsapp.db-shm", "whatsapp.db-journal"} {
		if err := os.WriteFile(filepath.Join("store", name), nil, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir("store/chat", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("store/chat/media.ogg", []byte("audio"), 0644); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewMessageStore()
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	assertPrivateMode(t, "store", 0700)
	assertPrivateMode(t, "store/chat", 0700)
	assertPrivateMode(t, "store/chat/media.ogg", 0600)
	for _, name := range []string{"messages.db", "whatsapp.db", "whatsapp.db-wal", "whatsapp.db-shm", "whatsapp.db-journal"} {
		assertPrivateMode(t, filepath.Join("store", name), 0600)
	}
}

func TestPrivateMediaAndPairingQR(t *testing.T) {
	setupChatStore(t)
	if err := privateDirectory("store/chat"); err != nil {
		t.Fatal(err)
	}
	assertPrivateMode(t, "store/chat", 0700)
	if err := privateFile("store/chat/media.ogg", []byte("audio")); err != nil {
		t.Fatal(err)
	}
	assertPrivateMode(t, "store/chat/media.ogg", 0600)
	if err := os.Chmod("store/chat/media.ogg", 0644); err != nil {
		t.Fatal(err)
	}
	if err := privateFile("store/chat/media.ogg", []byte("new")); err != nil {
		t.Fatal(err)
	}
	assertPrivateMode(t, "store/chat/media.ogg", 0600)
	data, err := os.ReadFile("store/chat/media.ogg")
	if err != nil || string(data) != "new" {
		t.Fatalf("rewrite=%q, %v", data, err)
	}
	if err := updatePairingQR([]byte("QR PNG")); err != nil {
		t.Fatal(err)
	}
	assertPrivateMode(t, "store/whatsapp-qr.png", 0600)
	if err := updatePairingQR(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat("store/whatsapp-qr.png"); !os.IsNotExist(err) {
		t.Fatalf("paired QR still exists: %v", err)
	}
	if err := updatePairingQR(nil); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteSidecarPermissions(t *testing.T) {
	for _, mode := range []string{"DELETE", "WAL"} {
		t.Run(mode, func(t *testing.T) {
			s := setupChatStore(t)
			if _, err := s.db.Exec("PRAGMA journal_mode=" + mode); err != nil {
				t.Fatal(err)
			}
			tx, err := s.db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if _, err := tx.Exec("INSERT INTO chats(jid) VALUES ('chat')"); err != nil {
				t.Fatal(err)
			}
			if mode == "WAL" {
				assertPrivateMode(t, "store/messages.db-wal", 0600)
				assertPrivateMode(t, "store/messages.db-shm", 0600)
			} else {
				assertPrivateMode(t, "store/messages.db-journal", 0600)
			}
		})
	}
}

func TestSessionDatabasePermissions(t *testing.T) {
	setupChatStore(t)
	if err := prepareDatabase("store/whatsapp.db"); err != nil {
		t.Fatal(err)
	}
	c, err := sqlstore.New(context.Background(), "sqlite3", "file:store/whatsapp.db?_foreign_keys=on", waLog.Noop)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	assertPrivateMode(t, "store/whatsapp.db", 0600)
	if _, err := c.GetFirstDevice(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadCreatesPrivateDirectory(t *testing.T) {
	s := setupChatStore(t)
	if err := s.EnsureChat("chat", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.StoreMessage("msg", "chat", "sender", "", time.Now(), false, "audio", "audio.ogg", "", nil, nil, nil, 0); err != nil {
		t.Fatal(err)
	}
	downloadMedia(nil, s, "msg", "chat")
	assertPrivateMode(t, "store/chat", 0700)
}
