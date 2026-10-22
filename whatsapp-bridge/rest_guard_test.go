package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func guardedMux() *http.ServeMux {
	mux := http.NewServeMux()
	ok := func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
		}
	}
	for _, p := range []string{"/api/send", "/api/messages", "/qr"} {
		mux.HandleFunc(p, ok)
	}
	return mux
}

func guardCode(h http.Handler, path, host, origin, auth, body string) int {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Host = host
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

// Without a token the loopback API must refuse what a web page can send: a cross-site POST carries Origin,
// a DNS-rebinding page carries its own Host.
func TestAPIGuardWithoutTokenRefusesBrowserRequests(t *testing.T) {
	h := apiGuard("", "127.0.0.1", 8080, false, guardedMux())
	cases := []struct {
		host, origin string
		want         int
	}{
		{"127.0.0.1:8080", "", 200},
		{"localhost:8080", "", 200},
		{"[::1]:8080", "", 200},
		{"127.0.0.1:8080", "https://evil.example", 403},
		{"127.0.0.1:8080", "null", 403},
		{"rebind.evil.example:8080", "", 403},
		{"127.0.0.1:9999", "", 403},
	}
	for _, c := range cases {
		if got := guardCode(h, "/api/send", c.host, c.origin, "", "{}"); got != c.want {
			t.Errorf("host=%s origin=%q: got %d want %d", c.host, c.origin, got, c.want)
		}
	}
	if got := guardCode(h, "/qr", "rebind.evil.example", "https://evil.example", "", ""); got != 200 {
		t.Errorf("/qr is outside /api and stays open: got %d", got)
	}
}

func TestAPIGuardTokenIsTheWholeCheck(t *testing.T) {
	h := apiGuard("s3cret", "100.64.0.1", 8080, false, guardedMux())
	if got := guardCode(h, "/api/send", "bridge:8080", "", "Bearer s3cret", "{}"); got != 200 {
		t.Errorf("right token from a non-loopback host: got %d", got)
	}
	for _, auth := range []string{"", "Bearer s3cre", "Bearer s3cret ", "s3cret"} {
		if got := guardCode(h, "/api/send", "100.64.0.1:8080", "", auth, "{}"); got != 401 {
			t.Errorf("auth %q: got %d want 401", auth, got)
		}
	}
}

func TestAPIGuardReadOnlyRefusesWrites(t *testing.T) {
	h := apiGuard("", "127.0.0.1", 8080, true, guardedMux())
	if got := guardCode(h, "/api/send", "127.0.0.1:8080", "", "", "{}"); got != 403 {
		t.Errorf("send in read-only mode: got %d want 403", got)
	}
	if got := guardCode(h, "/api/messages", "127.0.0.1:8080", "", "", "{}"); got != 200 {
		t.Errorf("messages in read-only mode: got %d want 200", got)
	}
}

func TestAPIGuardCapsTheBody(t *testing.T) {
	h := apiGuard("", "127.0.0.1", 8080, false, guardedMux())
	if got := guardCode(h, "/api/send", "127.0.0.1:8080", "", "", strings.Repeat("x", maxAPIBody+1)); got != 413 {
		t.Errorf("oversized body: got %d want 413", got)
	}
}

// Every read route in the read-only list must exist, so a renamed route cannot silently fall out of it.
func TestReadOnlyRoutesExist(t *testing.T) {
	src, err := readGoSources()
	if err != nil {
		t.Fatal(err)
	}
	for path := range readOnlyAPI {
		if !strings.Contains(src, `HandleFunc("`+path+`"`) {
			t.Errorf("read-only route %s is not registered anywhere", path)
		}
	}
}

func readGoSources() (string, error) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			return "", err
		}
		b.Write(data)
	}
	return b.String(), nil
}
