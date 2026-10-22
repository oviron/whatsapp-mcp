package main

import (
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

type fakeSession struct {
	paired, connected bool
	connects          int
}

func (f *fakeSession) Connect() error    { f.connects++; f.connected = true; return nil }
func (f *fakeSession) IsConnected() bool { return f.connected }
func (f *fakeSession) IsPaired() bool    { return f.paired }

type supervisorHarness struct {
	*connectionSupervisor
	session *fakeSession
	clock   time.Time
	timers  []time.Duration
	pending []func()
	exited  []int
}

func newSupervisorHarness() *supervisorHarness {
	h := &supervisorHarness{session: &fakeSession{paired: true}, clock: time.Unix(1_000_000, 0)}
	h.connectionSupervisor = &connectionSupervisor{
		session: h.session,
		log:     waLog.Noop,
		exit:    func(code int) { h.exited = append(h.exited, code) },
		now:     func() time.Time { return h.clock },
		after: func(d time.Duration, f func()) {
			h.timers = append(h.timers, d)
			h.pending = append(h.pending, f)
		},
	}
	return h
}

// fire advances the clock by the last scheduled delay and runs that timer.
func (h *supervisorHarness) fire() {
	h.clock = h.clock.Add(h.timers[len(h.timers)-1])
	h.pending[len(h.pending)-1]()
}

func TestStreamReplacedReconnectsWithBackoff(t *testing.T) {
	h := newSupervisorHarness()
	for _, want := range []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute} {
		h.handle(&events.StreamReplaced{})
		if got := h.timers[len(h.timers)-1]; got != want {
			t.Fatalf("pause = %v, want %v", got, want)
		}
		h.reconnect() // the watchdog must not jump the pause
		if h.session.connects != 0 {
			t.Fatal("reconnected during the pause")
		}
	}
	h.fire()
	if h.session.connects != 1 {
		t.Fatalf("connects = %d, want 1 after the pause", h.session.connects)
	}

	h.session.connected = false
	h.clock = h.clock.Add(2 * time.Hour)
	h.handle(&events.StreamReplaced{})
	if got := h.timers[len(h.timers)-1]; got != 30*time.Second {
		t.Fatalf("pause after a quiet hour = %v, want 30s", got)
	}
}

func TestStreamReplacedPauseIsCapped(t *testing.T) {
	h := newSupervisorHarness()
	for range 20 {
		h.handle(&events.StreamReplaced{})
	}
	if got := h.timers[len(h.timers)-1]; got != streamReplacedMaxPause {
		t.Fatalf("pause = %v, want %v", got, streamReplacedMaxPause)
	}
}

func TestTemporaryBanWaitsForExpiry(t *testing.T) {
	for name, tc := range map[string]struct{ expire, want time.Duration }{
		"known expiry":   {expire: 10 * time.Minute, want: 10 * time.Minute},
		"unknown expiry": {want: time.Hour},
	} {
		t.Run(name, func(t *testing.T) {
			h := newSupervisorHarness()
			h.handle(&events.TemporaryBan{Expire: tc.expire})
			h.reconnect()
			if h.session.connects != 0 || h.timers[0] != tc.want {
				t.Fatalf("connects = %d, timer = %v; want 0 and %v", h.session.connects, h.timers[0], tc.want)
			}
			h.fire()
			if h.session.connects != 1 {
				t.Fatalf("connects = %d after the ban, want 1", h.session.connects)
			}
		})
	}
}

func TestClientOutdatedStopsReconnecting(t *testing.T) {
	h := newSupervisorHarness()
	h.handle(&events.ClientOutdated{})
	h.reconnect()
	if h.session.connects != 0 || len(h.exited) != 0 {
		t.Fatalf("connects = %d, exits = %v; want neither", h.session.connects, h.exited)
	}
}

func TestLoggedOutExitsNonZeroAfterDelay(t *testing.T) {
	h := newSupervisorHarness()
	h.handle(&events.LoggedOut{Reason: events.ConnectFailureLoggedOut})
	if len(h.exited) != 0 || h.timers[0] != loggedOutExitDelay {
		t.Fatalf("exits = %v, timer = %v; want a delayed exit", h.exited, h.timers)
	}
	h.fire()
	if len(h.exited) != 1 || h.exited[0] != 1 {
		t.Fatalf("exits = %v, want [1]", h.exited)
	}
}

func TestWatchdogReconnectsOnlyPairedOfflineClient(t *testing.T) {
	for name, tc := range map[string]struct {
		paired, connected bool
		want              int
	}{
		"paired offline":   {paired: true, want: 1},
		"paired connected": {paired: true, connected: true},
		"not paired":       {},
	} {
		t.Run(name, func(t *testing.T) {
			h := newSupervisorHarness()
			h.session.paired, h.session.connected = tc.paired, tc.connected
			h.reconnect()
			if h.session.connects != tc.want {
				t.Fatalf("connects = %d, want %d", h.session.connects, tc.want)
			}
		})
	}
}

func TestHandleMessageReportsFailedWrites(t *testing.T) {
	text := &waProto.Message{Conversation: proto.String("text")}
	edit := &waProto.Message{ProtocolMessage: &waProto.ProtocolMessage{
		Type: waProto.ProtocolMessage_MESSAGE_EDIT.Enum(), Key: &waProto.MessageKey{ID: proto.String("old")}, EditedMessage: text,
	}}
	for name, tc := range map[string]struct {
		body      *waProto.Message
		failWrite bool
		want      bool
	}{
		"stored":       {body: text, want: true},
		"nothing":      {body: &waProto.Message{}, want: true},
		"write fails":  {body: text, failWrite: true},
		"edit applied": {body: edit, want: true},
		"edit fails":   {body: edit, failWrite: true},
	} {
		t.Run(name, func(t *testing.T) {
			s := setupChatStore(t)
			chat := types.NewJID("111", types.DefaultUserServer)
			if err := s.StoreChat(chat.String(), "Contact", time.Unix(100, 0)); err != nil {
				t.Fatal(err)
			}
			if err := s.StoreMessage("old", chat.String(), "111", "before", time.Unix(100, 0), false, "", "", "", nil, nil, nil, 0); err != nil {
				t.Fatal(err)
			}
			if tc.failWrite {
				if _, err := s.db.Exec(`
					CREATE TRIGGER fail_insert BEFORE INSERT ON messages BEGIN SELECT RAISE(FAIL, 'test write failure'); END;
					CREATE TRIGGER fail_update BEFORE UPDATE ON messages BEGIN SELECT RAISE(FAIL, 'test write failure'); END`); err != nil {
					t.Fatal(err)
				}
			}
			got := handleMessage(&whatsmeow.Client{}, s, &events.Message{
				Info:    types.MessageInfo{ID: "live", Timestamp: time.Unix(200, 0), MessageSource: types.MessageSource{Chat: chat, Sender: chat}},
				Message: tc.body,
			}, waLog.Noop)
			if got != tc.want {
				t.Fatalf("handleMessage = %v, want %v", got, tc.want)
			}
		})
	}
}
