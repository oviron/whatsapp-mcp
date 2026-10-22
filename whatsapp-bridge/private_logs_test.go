package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	waBinary "go.mau.fi/whatsmeow/binary"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

func capturePrivateLog(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old; r.Close() }()
	out := make(chan string, 1)
	go func() { b, _ := io.ReadAll(r); out <- string(b) }()
	f()
	w.Close()
	return <-out
}

func TestPrivateMessageLogs(t *testing.T) {
	for _, setting := range []string{"", "0", "false", "1", "true", "yes", "on", " TRUE "} {
		t.Run("content="+setting, func(t *testing.T) {
			t.Setenv("WHATSAPP_LOG_CONTENT", setting)
			t.Setenv("WEBHOOK_URL", "")
			s := setupChatStore(t)
			chat := types.NewJID("123", types.DefaultUserServer)
			if err := s.StoreChat(chat.String(), "PRIVATE NAME", time.Unix(1, 0)); err != nil {
				t.Fatal(err)
			}
			msg := &waProto.Message{ImageMessage: &waProto.ImageMessage{
				Caption: proto.String("PRIVATE MESSAGE"), MediaKey: []byte("PRIVATE MEDIA KEY"),
				URL: proto.String("https://media.invalid/SECRET"), DirectPath: proto.String("/SECRET_PATH"),
			}}
			for _, history := range []bool{false, true} {
				out := capturePrivateLog(t, func() {
					logger := waLog.Stdout("Test", "INFO", false)
					if history {
						handleHistorySync(nil, s, &events.HistorySync{Data: &waProto.HistorySync{Conversations: []*waProto.Conversation{{
							ID: proto.String(chat.String()), Messages: []*waProto.HistorySyncMsg{{Message: &waProto.WebMessageInfo{
								Key: &waProto.MessageKey{ID: proto.String("history")}, MessageTimestamp: proto.Uint64(100), Message: msg,
							}}},
						}}}}, logger)
					} else {
						handleMessage(&whatsmeow.Client{}, s, &events.Message{Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: chat, Sender: chat}, ID: "live", Timestamp: time.Unix(100, 0)}, Message: msg}, logger)
					}
				})
				if strings.Contains(out, "PRIVATE MESSAGE") != (setting == "1" || setting == "true" || setting == "yes" || setting == "on" || setting == " TRUE ") {
					t.Errorf("history=%v content log: %s", history, out)
				}
				for _, secret := range []string{"PRIVATE MEDIA KEY", "80 82 73 86 65 84 69", "media.invalid", "/SECRET_PATH", "PRIVATE NAME"} {
					if strings.Contains(out, secret) {
						t.Errorf("history=%v leaked %q: %s", history, secret, out)
					}
				}
				if !strings.Contains(out, chat.String()) || !strings.Contains(out, "image") {
					t.Errorf("missing metadata: %s", out)
				}
			}
		})
	}
}

func TestUploadAndLibraryLogsNeverIncludeSecrets(t *testing.T) {
	for _, flag := range []string{"", "1"} {
		t.Setenv("WHATSAPP_LOG_CONTENT", flag)
		out := capturePrivateLog(t, func() {
			logMediaUpload(whatsmeow.UploadResponse{MediaKey: []byte("PRIVATE_KEY"), URL: "https://media.invalid/SECRET", FileLength: 42})
			logger := privateLibraryLogger{waLog.Stdout("Library", "INFO", false)}
			logger.Warnf("retry %d failed: %v", 3, errors.New("connection refused"))
			logger.Infof("status=%s bytes=%v", "offline", []byte("PRIVATE_KEY"))
			node := waBinary.Node{Tag: "message", Content: []byte("PRIVATE MESSAGE")}
			logger.Sub("Download").Errorf("node=%v pointer=%+v proto=%v", node, &node, &waProto.Message{Conversation: proto.String("PRIVATE MESSAGE")})
			logger.Warnf("fetch https://media.invalid/SECRET: %v", fmt.Errorf("Get https://cdn.invalid/TOKEN: timeout"))
			logger.Debugf("debug PRIVATE MESSAGE")
		})
		for _, secret := range []string{"PRIVATE_KEY", "80 82 73", "PRIVATE MESSAGE", "media.invalid", "cdn.invalid", "TOKEN", "SECRET"} {
			if strings.Contains(out, secret) {
				t.Errorf("leaked %q: %s", secret, out)
			}
		}
		for _, want := range []string{"bytes=42", "retry 3 failed: connection refused", "status=offline", "<[]uint8>", "<binary.Node>", "<*binary.Node>", "<*waE2E.Message>", "<url>", "timeout"} {
			if !strings.Contains(out, want) {
				t.Errorf("missing %q: %s", want, out)
			}
		}
	}
}

func TestOutboundMetadataLog(t *testing.T) {
	for _, flag := range []string{"", "1"} {
		t.Setenv("WHATSAPP_LOG_CONTENT", flag)
		out := capturePrivateLog(t, func() {
			logMessage(time.Unix(100, 0), true, "123@s.whatsapp.net", "999@s.whatsapp.net", "text", "PRIVATE MESSAGE")
		})
		if strings.Contains(out, "PRIVATE MESSAGE") != (flag == "1") {
			t.Errorf("content=%q: %s", flag, out)
		}
		for _, field := range []string{"direction=out", "123@s.whatsapp.net", "999@s.whatsapp.net", "length=15", "text"} {
			if !strings.Contains(out, field) {
				t.Errorf("missing %s: %s", field, out)
			}
		}
	}
}

type privateWebhookTransport func(*http.Request) (*http.Response, error)

func (f privateWebhookTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestWebhookAndMediaErrorsRedactURLs(t *testing.T) {
	cause := errors.New("connection refused")
	err := fmt.Errorf("download failed: %w", &url.Error{Op: "Get", URL: "https://cdn.invalid/file?signature=SECRET", Err: cause})
	s, chat, _, _ := pipelineMedia(t)
	d := &pipelineDownloader{write: func(whatsmeow.File) error { return err }}
	_, _, _, _, got := downloadMedia(d, s, "media", chat)
	if got == nil || !errors.Is(got, cause) || !strings.Contains(got.Error(), "connection refused") || strings.Contains(got.Error(), "SECRET") || strings.Contains(got.Error(), "cdn.invalid") {
		t.Fatalf("media error lost cause or leaked URL: %v", got)
	}
	// Nested URL errors must be redacted without discarding the useful reason.
	out := redactURLErr(err).Error()
	if strings.Contains(out, "SECRET") || !strings.Contains(out, "connection refused") {
		t.Fatalf("redacted error: %s", out)
	}
	previous := webhookBackoff
	webhookBackoff = 0
	defer func() { webhookBackoff = previous }()
	dispatcher := webhookDispatcher{url: "https://hook.invalid/SECRET", client: &http.Client{Transport: privateWebhookTransport(func(*http.Request) (*http.Response, error) { return nil, err })}}
	out = capturePrivateLog(t, func() { dispatcher.deliver(samplePayload()) })
	if strings.Contains(out, "SECRET") || !strings.Contains(out, "connection refused") || !strings.Contains(out, "dropped after 3 attempts") {
		t.Fatalf("webhook error: %s", out)
	}
	plain := errors.New("permission denied")
	if got := redactURLErr(plain); got != plain {
		t.Fatalf("ordinary error changed: %v", got)
	}
}

func TestTerminalQRRequiresTTY(t *testing.T) {
	for _, tty := range []bool{false, true} {
		checked := false
		out := capturePrivateLog(t, func() { printTerminalQR("test-pairing-credential", func() bool { checked = true; return tty }) })
		if !checked || (out != "") != tty {
			t.Errorf("tty=%v checked=%v output=%q", tty, checked, out)
		}
	}
	// A code-based pairing suppresses both terminal and file QR presentation.
	p := pairingPrompt{phone: "test", request: func(string) (string, error) { return "ABCD-EFGH", nil }}
	for range 2 {
		_, showQR, err := p.onQR()
		if err != nil || showQR {
			t.Fatalf("QR shown during phone pairing: %v, %v", showQR, err)
		}
	}
}

func TestStdoutPipeAndNullAreNotTerminals(t *testing.T) {
	out := capturePrivateLog(t, func() {
		if stdoutIsTerminal() {
			t.Error("pipe treated as a terminal")
		}
	})
	if out != "" {
		t.Fatalf("unexpected output: %q", out)
	}
	file, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	previous := os.Stdout
	os.Stdout = file
	defer func() { os.Stdout = previous }()
	if stdoutIsTerminal() {
		t.Error("null device treated as a terminal")
	}
}
