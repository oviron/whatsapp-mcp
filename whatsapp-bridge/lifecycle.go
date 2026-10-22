package main

import (
	"errors"
	"os"
	"runtime/debug"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

const (
	watchdogInterval       = time.Minute
	streamReplacedMinPause = 30 * time.Second
	streamReplacedMaxPause = 30 * time.Minute
	logoutPollInterval     = 500 * time.Millisecond
	logoutDeleteTimeout    = 30 * time.Second
)

type sessionClient interface {
	Connect() error
	IsConnected() bool
	IsPaired() bool
}

type whatsmeowSession struct{ *whatsmeow.Client }

func (c whatsmeowSession) IsPaired() bool { return c.Store.ID != nil }

// connectionSupervisor handles the disconnects whatsmeow does not come back from on its own
// (stream replaced, ban, outdated client, logout), and a watchdog reconnects a paired client
// that stayed offline anyway. Without it the process lives on with a dead session, so neither
// launchd KeepAlive nor systemd Restart= ever kicks in.
type connectionSupervisor struct {
	session        sessionClient
	sessionDeleted func() bool
	log            waLog.Logger
	exit           func(code int)
	now            func() time.Time
	after          func(time.Duration, func())

	mu            sync.Mutex
	holdUntil     time.Time // no reconnect before this
	stopped       bool      // needs a person (rebuild, new QR); reconnecting would only repeat the refusal
	replacedPause time.Duration
	lastReplaced  time.Time
}

func newConnectionSupervisor(client *whatsmeow.Client, log waLog.Logger, sessionDeleted func() bool) *connectionSupervisor {
	return &connectionSupervisor{
		session:        whatsmeowSession{client},
		sessionDeleted: sessionDeleted,
		log:            log,
		exit:           os.Exit,
		now:            time.Now,
		after:          func(d time.Duration, f func()) { time.AfterFunc(d, f) },
	}
}

func (s *connectionSupervisor) handle(evt any) {
	switch v := evt.(type) {
	case *events.Disconnected:
		s.log.Warnf("Disconnected from WhatsApp; whatsmeow reconnects automatically")
	case *events.StreamReplaced:
		// Two bridges on one session keep replacing each other, which risks a ban: back off.
		s.mu.Lock()
		if s.now().Sub(s.lastReplaced) > time.Hour {
			s.replacedPause = 0
		}
		s.replacedPause = min(max(2*s.replacedPause, streamReplacedMinPause), streamReplacedMaxPause)
		s.lastReplaced = s.now()
		pause := s.replacedPause
		s.holdUntil = s.now().Add(pause)
		s.mu.Unlock()
		s.log.Errorf("Another client took over this WhatsApp session (a second bridge on the same store?); reconnecting in %v", pause)
		s.after(pause, s.reconnect)
	case *events.TemporaryBan:
		wait := v.Expire
		if wait <= 0 {
			wait = time.Hour
		}
		s.mu.Lock()
		s.holdUntil = s.now().Add(wait)
		s.mu.Unlock()
		s.log.Errorf("%s. Reconnecting in %v; reads keep working from the local database.", v.String(), wait)
		s.after(wait, s.reconnect)
	case *events.ClientOutdated:
		s.mu.Lock()
		s.stopped = true
		s.mu.Unlock()
		s.log.Errorf("WhatsApp rejected this client version: update go.mau.fi/whatsmeow and rebuild the bridge. Reads keep working; nothing is sent or received until then.")
	case *events.ConnectFailure:
		s.log.Errorf("WhatsApp refused the connection (%s); retrying every %v", v.Reason, watchdogInterval)
	case *events.LoggedOut:
		s.mu.Lock()
		s.stopped = true
		s.mu.Unlock()
		s.log.Errorf("Logged out of WhatsApp (%s); the session is removed. Exiting so the service manager restarts the bridge with a new QR code.", v.Reason)
		s.exitAfterLogout(0)
	}
}

// exitAfterLogout waits for whatsmeow to delete the session, which it does only after dispatching
// LoggedOut: exiting earlier restarts into the dead session instead of a QR code.
func (s *connectionSupervisor) exitAfterLogout(waited time.Duration) {
	if s.sessionDeleted() {
		s.exit(1)
		return
	}
	if waited >= logoutDeleteTimeout {
		s.log.Errorf("The logged-out session is still in store/whatsapp.db after %v; exiting anyway", waited)
		s.exit(1)
		return
	}
	s.after(logoutPollInterval, func() { s.exitAfterLogout(waited + logoutPollInterval) })
}

// failOnPanic reports a panicking handler as failed: whatsmeow recovers the panic itself but then
// acks the message as handled, dropping it for good.
func failOnPanic(log waLog.Logger, handler func(any) bool) func(any) bool {
	return func(evt any) (ok bool) {
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("Event handler panicked on %T: %v\n%s", evt, r, debug.Stack())
				ok = false
			}
		}()
		return handler(evt)
	}
}

func (s *connectionSupervisor) reconnect() {
	s.mu.Lock()
	blocked := s.stopped || s.now().Before(s.holdUntil)
	s.mu.Unlock()
	if blocked || !s.session.IsPaired() || s.session.IsConnected() {
		return
	}
	s.log.Warnf("Paired but not connected to WhatsApp; reconnecting")
	if err := s.session.Connect(); err != nil && !errors.Is(err, whatsmeow.ErrAlreadyConnected) {
		s.log.Warnf("Reconnect failed: %v", err)
	}
}

func (s *connectionSupervisor) watch(interval time.Duration) {
	go func() {
		for range time.Tick(interval) {
			s.reconnect()
		}
	}()
}
