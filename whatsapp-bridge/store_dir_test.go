package main

import (
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waMmsRetry"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"go.mau.fi/whatsmeow/util/gcmutil"
	"go.mau.fi/whatsmeow/util/hkdfutil"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

func TestStoreDirDatabasesMediaAndQR(t *testing.T) {
	external := filepath.Join(t.TempDir(), "external [data] #%")
	if runtime.GOOS != "windows" {
		external += "?"
	}
	for _, configured := range []string{"", external} {
		t.Run(configured, func(t *testing.T) {
			t.Setenv("WHATSAPP_STORE_DIR", configured)
			s := setupChatStore(t)
			root := configured
			if root == "" {
				root = "store"
			}
			assertPrivateMode(t, root, 0700)
			assertPrivateMode(t, filepath.Join(root, "messages.db"), 0600)
			mustExec(t, s, "INSERT INTO chats(jid) VALUES ('probe')")
			read, err := openUnaccentMessagesDB()
			if err != nil {
				t.Fatal(err)
			}
			defer read.Close()
			var jid string
			if err := read.QueryRow("SELECT jid FROM chats").Scan(&jid); err != nil || jid != "probe" {
				t.Fatalf("messages read connection: %q, %v", jid, err)
			}
			sessionPath := filepath.Join(root, "whatsapp.db")
			if err := prepareDatabase(storePath("whatsapp.db")); err != nil {
				t.Fatal(err)
			}
			container, err := sqlstore.New(context.Background(), "sqlite3", storeDSN("whatsapp.db", "_foreign_keys=on"), waLog.Noop)
			if err != nil {
				t.Fatal(err)
			}
			defer container.Close()
			session, err := openStoreDBReadOnly()
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			if err := session.QueryRow("SELECT COUNT(*) FROM whatsmeow_device").Scan(new(int)); err != nil {
				t.Fatal(err)
			}
			assertPrivateMode(t, sessionPath, 0600)
			if err := updatePairingQR([]byte("png")); err != nil {
				t.Fatal(err)
			}
			qr := filepath.Join(root, "whatsapp-qr.png")
			assertPrivateMode(t, qr, 0600)
			if err := updatePairingQR(nil); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(qr); !os.IsNotExist(err) {
				t.Fatalf("QR not removed: %v", err)
			}
			data := []byte("media")
			hash := sha256.Sum256(data)
			if err := s.EnsureChat("chat", time.Now()); err != nil {
				t.Fatal(err)
			}
			if err := s.StoreMessage("msg", "chat", "sender", "", time.Now(), false, "audio", "a.ogg", "url", []byte("key"), hash[:], []byte("enc"), uint64(len(data))); err != nil {
				t.Fatal(err)
			}
			ok, _, _, got, err := downloadMedia(&pipelineDownloader{data: data}, s, "msg", "chat")
			want, _ := filepath.Abs(filepath.Join(root, "chat", "msg_a.ogg"))
			if !ok || err != nil || got != want {
				t.Fatalf("download = %v, %q, %v; want %q", ok, got, err, want)
			}
			assertPrivateMode(t, filepath.Dir(want), 0700)
			assertPrivateMode(t, want, 0600)
			if err := os.Remove(want); err != nil {
				t.Fatal(err)
			}
			oldCache := mediaRetryCache
			mediaRetryCache = &retryCache{m: make(map[string]mediaRetryEntry)}
			t.Cleanup(func() { mediaRetryCache = oldCache })
			mediaRetryCache.store("msg", mediaRetryEntry{chatJID: "chat", mediaKey: []byte("key"),
				mediaType: "audio", filename: "a.ogg", fileSHA256: hash[:], fileLength: uint64(len(data)), storedAt: time.Now()})
			notification, err := proto.Marshal(&waMmsRetry.MediaRetryNotification{
				Result: waMmsRetry.MediaRetryNotification_SUCCESS.Enum(), DirectPath: proto.String("/fresh")})
			if err != nil {
				t.Fatal(err)
			}
			iv := make([]byte, 12)
			key := hkdfutil.SHA256([]byte("key"), nil, []byte("WhatsApp Media Retry Notification"), 32)
			ciphertext, err := gcmutil.Encrypt(key, iv, notification, []byte("msg"))
			if err != nil {
				t.Fatal(err)
			}
			handleMediaRetry(&pipelineDownloader{data: data}, s, &events.MediaRetry{
				MessageID: "msg", ChatID: types.NewJID("chat", ""), Ciphertext: ciphertext, IV: iv}, waLog.Noop)
			if got, err := os.ReadFile(want); err != nil || string(got) != string(data) {
				t.Fatalf("retry media: %q, %v", got, err)
			}
			if configured != "" {
				if _, err := os.Stat("store"); !os.IsNotExist(err) {
					t.Fatalf("default store created: %v", err)
				}
			}
		})
	}
}

func TestStoreDirLIDOrphanMedia(t *testing.T) {
	root := filepath.Join(t.TempDir(), "external [store]")
	t.Setenv("WHATSAPP_STORE_DIR", root)
	s := setupChatStore(t)
	mediaFile(t, filepath.Join(root, testLID, "msg_audio.ogg"))
	migrateLIDChats(lidClient(map[string]string{"999": "222"}), s, waLog.Noop)
	assertPrivateMode(t, filepath.Join(root, testPN), 0700)
	if data, err := os.ReadFile(filepath.Join(root, testPN, "msg_audio.ogg")); err != nil || string(data) != "media" {
		t.Fatalf("LID media not moved: %q, %v", data, err)
	}
}

func TestStoreDirExistingPermissions(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WHATSAPP_STORE_DIR", root)
	chat := filepath.Join(root, "chat")
	if err := os.Mkdir(chat, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"whatsapp.db", "whatsapp.db-wal", "whatsapp.db-shm", "whatsapp.db-journal", "chat/audio.ogg"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(root, 0755); err != nil {
		t.Fatal(err)
	}
	setupChatStore(t)
	assertPrivateMode(t, root, 0700)
	assertPrivateMode(t, chat, 0700)
	for _, name := range []string{"messages.db", "whatsapp.db", "whatsapp.db-wal", "whatsapp.db-shm", "whatsapp.db-journal", "chat/audio.ogg"} {
		assertPrivateMode(t, filepath.Join(root, name), 0600)
	}
}

func TestStoreDirMediaRoots(t *testing.T) {
	home, _, _, _ := mediaFixture(t)
	root := filepath.Join(home, ".private", "history")
	t.Setenv("WHATSAPP_STORE_DIR", root)
	media := mediaFile(t, filepath.Join(root, "chat", "a.ogg"))
	if data, err := readMediaFile(media); err != nil || string(data) != "media" {
		t.Fatalf("downloaded media: %q, %v", data, err)
	}
	for _, roots := range []string{"", root, home} {
		t.Setenv("WHATSAPP_MEDIA_DIRS", roots)
		expectMediaDenied(t, mediaFile(t, filepath.Join(root, "whatsapp.db")))
		expectMediaDenied(t, mediaFile(t, filepath.Join(root, "messages.db-wal")))
	}
}
