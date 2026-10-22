package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestParseBridgeOptions(t *testing.T) {
	t.Setenv("WHATSAPP_PAIR_PHONE", "5511999990000")
	o, err := parseBridgeOptions(nil)
	if err != nil || o.pairPhone != "5511999990000" || o.logout {
		t.Fatalf("defaults = %+v, %v; want the phone from the environment", o, err)
	}
	o, err = parseBridgeOptions([]string{"-logout", "-pair-phone", "79990001122"})
	if err != nil || o.pairPhone != "79990001122" || !o.logout {
		t.Fatalf("flags = %+v, %v", o, err)
	}
	if _, err := parseBridgeOptions([]string{"-bogus"}); err == nil {
		t.Fatal("unknown flag accepted")
	}
}

type fakeLogout struct {
	paired, online bool
	connectErr     error
	calls          []string
}

func (f *fakeLogout) IsPaired() bool { return f.paired }
func (f *fakeLogout) Connect() error { f.calls = append(f.calls, "connect"); return f.connectErr }
func (f *fakeLogout) WaitForConnection(time.Duration) bool {
	f.calls = append(f.calls, "wait")
	return f.online
}
func (f *fakeLogout) Logout(context.Context) error { f.calls = append(f.calls, "logout"); return nil }

func TestLogoutBridge(t *testing.T) {
	boom := errors.New("boom")
	for name, tc := range map[string]struct {
		c        fakeLogout
		unlinked bool
		err      error
		calls    int
	}{
		"not paired":    {},
		"unlinks":       {c: fakeLogout{paired: true, online: true}, unlinked: true, calls: 3},
		"offline":       {c: fakeLogout{paired: true}, err: errLogoutOffline, calls: 2},
		"connect fails": {c: fakeLogout{paired: true, connectErr: boom}, err: boom, calls: 1},
	} {
		t.Run(name, func(t *testing.T) {
			unlinked, err := logoutBridge(&tc.c)
			if unlinked != tc.unlinked || !errors.Is(err, tc.err) || len(tc.c.calls) != tc.calls {
				t.Fatalf("unlinked = %v, err = %v, calls = %v; want %v, %v, %d calls", unlinked, err, tc.c.calls, tc.unlinked, tc.err, tc.calls)
			}
		})
	}
}
