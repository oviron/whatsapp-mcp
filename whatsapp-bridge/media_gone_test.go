package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
)

func TestDownloadMediaPreservesClientError(t *testing.T) {
	store := setupChatStore(t)
	if err := store.StoreChat("chat", "test", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.StoreMessage("audio", "chat", "sender", "", time.Now(), false,
		"audio", "audio.ogg", "https://example.invalid/media", []byte{1}, []byte{2}, []byte{3}, 1); err != nil {
		t.Fatal(err)
	}
	success, _, _, _, err := downloadMedia(nil, store, "audio", "chat")
	if success || !errors.Is(err, whatsmeow.ErrClientIsNil) {
		t.Fatalf("success=%v, error=%v: expected wrapped client error", success, err)
	}
}

func TestDownloadMediaFailureReason(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"403", whatsmeow.ErrMediaDownloadFailedWith403, "media_gone"},
		{"404", whatsmeow.ErrMediaDownloadFailedWith404, "media_gone"},
		{"410", whatsmeow.ErrMediaDownloadFailedWith410, "media_gone"},
		{"unauthorized", whatsmeow.DownloadHTTPError{Response: &http.Response{StatusCode: 401}}, ""},
		{"rate limited", whatsmeow.DownloadHTTPError{Response: &http.Response{StatusCode: 429}}, ""},
		{"server error", whatsmeow.DownloadHTTPError{Response: &http.Response{StatusCode: 500}}, ""},
		{"plain error", errors.New("download failed: HTTP 403"), ""},
		{"no error", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, wrapped := range []bool{false, true} {
				err := tt.err
				if wrapped && err != nil {
					err = fmt.Errorf("failed to download media: %w", err)
				}
				reason := downloadMediaFailureReason(err)
				if reason != tt.want {
					t.Fatalf("wrapped=%v: reason=%q, want %q", wrapped, reason, tt.want)
				}
				data, err := json.Marshal(DownloadMediaResponse{Reason: reason})
				if err != nil {
					t.Fatal(err)
				}
				var body map[string]any
				if err := json.Unmarshal(data, &body); err != nil {
					t.Fatal(err)
				}
				if tt.want == "" {
					if _, exists := body["reason"]; exists {
						t.Fatalf("unexpected reason in %s", data)
					}
				} else if body["reason"] != tt.want {
					t.Fatalf("missing reason in %s", data)
				}
			}
		})
	}
}
