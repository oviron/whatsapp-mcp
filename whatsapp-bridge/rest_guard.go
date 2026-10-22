package main

import (
	"crypto/subtle"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// maxAPIBody caps /api/* request bodies: they carry JSON with paths and text, never file bytes.
const maxAPIBody = 1 << 20

// readOnlyAPI lists the /api/* routes that only read, for API_READONLY=true. Anything not listed is refused,
// so a route added later stays closed until someone decides it is a read.
var readOnlyAPI = map[string]bool{
	"/api/chat":                      true,
	"/api/chat/by_contact":           true,
	"/api/chats":                     true,
	"/api/chats/active":              true,
	"/api/contacts/chats":            true,
	"/api/contacts/last_interaction": true,
	"/api/contacts/search":           true,
	"/api/download":                  true, // fetches our own media into store/; nothing leaves the bridge
	"/api/drafts/list":               true,
	"/api/favorites/list":            true,
	"/api/favorites/suggest":         true,
	"/api/group_info":                true,
	"/api/is_on_whatsapp":            true,
	"/api/matters/list":              true,
	"/api/message_context":           true,
	"/api/messages":                  true,
	"/api/poll_votes":                true,
	"/api/resolve_contact":           true,
	"/api/review/next":               true,
	"/api/search_contacts":           true,
	"/api/sender_name":               true,
}

// apiGuard protects /api/* in front of the mux. With a token, the token is the whole check (constant time):
// a browser page cannot attach it. Without one the API is open to anything that reaches the loopback port,
// which includes web pages: a cross-site POST (CSRF) or a DNS-rebinding page that resolves its own name to
// 127.0.0.1. Browsers always send Origin on a cross-origin POST, and a rebinding page keeps its own Host,
// so tokenless requests must come without an Origin and name a loopback host or the bind address.
func apiGuard(token, bindAddr string, port int, readOnly bool, next http.Handler) http.Handler {
	allowedHosts := map[string]bool{}
	for _, h := range []string{"127.0.0.1", "localhost", "::1", bindAddr} {
		allowedHosts[net.JoinHostPort(h, strconv.Itoa(port))] = true
		if port == 80 { // clients leave the default port out of Host
			allowedHosts[strings.TrimSuffix(net.JoinHostPort(h, "80"), ":80")] = true
		}
	}
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		if token != "" {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		} else if r.Header.Get("Origin") != "" || !allowedHosts[r.Host] {
			http.Error(w, "Forbidden: browser requests and foreign Host headers are refused without API_AUTH_TOKEN", http.StatusForbidden)
			return
		}
		if readOnly && !readOnlyAPI[r.URL.Path] {
			http.Error(w, "Forbidden: the bridge runs with API_READONLY=true", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxAPIBody)
		next.ServeHTTP(w, r)
	})
}

func envBool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
