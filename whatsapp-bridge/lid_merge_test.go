package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

type mapLIDStore struct {
	store.LIDStore
	pn map[string]string // LID user -> phone number
}

func (m mapLIDStore) GetPNForLID(_ context.Context, lid types.JID) (types.JID, error) {
	if pn, ok := m.pn[lid.User]; ok {
		return types.NewJID(pn, types.DefaultUserServer), nil
	}
	return types.JID{}, nil
}

func lidClient(pn map[string]string) *whatsmeow.Client {
	return &whatsmeow.Client{Store: &store.Device{LIDs: mapLIDStore{pn: pn}}}
}

const (
	testLID = "999@lid"
	testPN  = "222@s.whatsapp.net"
)

func mustExec(t *testing.T, s *MessageStore, q string, args ...any) {
	t.Helper()
	if _, err := s.db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func seedLIDPair(t *testing.T, s *MessageStore) {
	t.Helper()
	if err := s.StoreChat(testPN, "Ana", ts("2026-10-20T10:00:00Z")); err != nil {
		t.Fatal(err)
	}
	if err := s.StoreChat(testLID, "", ts("2026-10-21T13:00:00+03:00")); err != nil {
		t.Fatal(err)
	}
	// The same audio under both keys; only the LID copy got transcribed.
	for _, c := range []struct{ chat, content string }{{testPN, ""}, {testLID, "transcribed"}} {
		mustExec(t, s, `INSERT INTO messages (id, chat_jid, sender, content, timestamp, is_from_me, media_type, filename, url, media_key, file_length)
			VALUES ('m1', ?, '222', ?, ?, 0, 'audio', 'a.ogg', 'u', x'01', 42)`, c.chat, c.content, ts("2026-10-20T09:00:00Z"))
	}
	if err := s.StoreMessage("m2", testLID, "222", "hello", ts("2026-10-21T10:00:00Z"), false, "", "", "", nil, nil, nil, 0); err != nil {
		t.Fatal(err)
	}
	mustExec(t, s, `INSERT INTO message_mentions (chat_jid, message_id, mentioned_user) VALUES (?, 'm2', '111')`, testLID)
	mustExec(t, s, `INSERT INTO polls (id, chat_jid, question, options, selectable_count) VALUES ('p1', ?, 'q', '[]', 1)`, testLID)
	mustExec(t, s, `INSERT INTO poll_votes (poll_id, chat_jid, voter, selected, timestamp) VALUES ('p1', ?, '111', '[]', ?)`, testLID, time.Unix(100, 0))
	mustExec(t, s, `INSERT INTO favorites (chat_jid, status, source, decided_at) VALUES (?, 'rejected', 'user', ?), (?, 'accepted', 'whatsapp', ?)`,
		testPN, time.Unix(100, 0), testLID, time.Unix(100, 0))
	mustExec(t, s, `INSERT INTO matters (chat_jid, owed_by, origin, origin_message_id, created_at, certainty, valid_until)
		VALUES (?, 'me', 'message', 'm2', ?, 'high', ?)`, testLID, time.Unix(100, 0), time.Unix(200, 0))
	mustExec(t, s, `INSERT INTO drafts (matter_id, chat_jid, text, state, written_at) VALUES (1, ?, 'draft', 'written', ?)`, testLID, time.Unix(100, 0))
	if err := os.MkdirAll(filepath.Join("store", testLID), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("store", testLID, "m1_a.ogg"), []byte("audio"), 0644); err != nil {
		t.Fatal(err)
	}
}

func countRows(t *testing.T, s *MessageStore, q string, args ...any) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func TestLIDMergeKeepsTranscriptionAndMovesEverything(t *testing.T) {
	s := setupChatStore(t)
	seedLIDPair(t, s)

	migrateLIDChats(lidClient(map[string]string{"999": "222"}), s, waLog.Noop)

	for _, table := range []string{"messages", "message_mentions", "polls", "poll_votes", "favorites", "matters", "drafts"} {
		if n := countRows(t, s, "SELECT COUNT(*) FROM "+table+" WHERE chat_jid = ?", testLID); n != 0 {
			t.Errorf("%s still has %d LID row(s)", table, n)
		}
		if n := countRows(t, s, "SELECT COUNT(*) FROM "+table+" WHERE chat_jid = ?", testPN); n == 0 {
			t.Errorf("%s has nothing under the phone number", table)
		}
	}
	if n := countRows(t, s, "SELECT COUNT(*) FROM chats WHERE jid = ?", testLID); n != 0 {
		t.Error("LID chat row left behind")
	}
	var content string
	if err := s.db.QueryRow("SELECT content FROM messages WHERE id = 'm1' AND chat_jid = ?", testPN).Scan(&content); err != nil || content != "transcribed" {
		t.Errorf("m1 content = %q, %v; want the LID copy's transcription", content, err)
	}
	var status string
	if err := s.db.QueryRow("SELECT status FROM favorites WHERE chat_jid = ?", testPN).Scan(&status); err != nil || status != "rejected" {
		t.Errorf("favorite = %q, %v; want the phone number's own decision", status, err)
	}
	assertChat(t, s, testPN, "Ana", ts("2026-10-21T10:00:00Z"))
	if _, err := os.Stat(filepath.Join("store", testPN, "m1_a.ogg")); err != nil {
		t.Errorf("cached audio not moved: %v", err)
	}
	if _, err := os.Stat(filepath.Join("store", testLID)); !os.IsNotExist(err) {
		t.Errorf("LID media directory left behind: %v", err)
	}
}

func TestLIDMergeRollsBackWholeChatOnFailure(t *testing.T) {
	s := setupChatStore(t)
	seedLIDPair(t, s)
	mustExec(t, s, `CREATE TRIGGER fail_drafts BEFORE UPDATE ON drafts BEGIN SELECT RAISE(FAIL, 'test failure'); END`)

	migrateLIDChats(lidClient(map[string]string{"999": "222"}), s, waLog.Noop)

	if n := countRows(t, s, "SELECT COUNT(*) FROM messages WHERE chat_jid = ?", testLID); n != 2 {
		t.Errorf("LID messages = %d, want both kept after the rollback", n)
	}
	if n := countRows(t, s, "SELECT COUNT(*) FROM messages WHERE chat_jid = ? AND content = 'transcribed'", testPN); n != 0 {
		t.Error("partial merge committed")
	}
	if _, err := os.Stat(filepath.Join("store", testLID, "m1_a.ogg")); err != nil {
		t.Errorf("media moved for a chat that was not merged: %v", err)
	}
}

func TestLIDMergeLeavesUnmappedChats(t *testing.T) {
	s := setupChatStore(t)
	seedLIDPair(t, s)

	migrateLIDChats(lidClient(nil), s, waLog.Noop)

	if n := countRows(t, s, "SELECT COUNT(*) FROM messages WHERE chat_jid = ?", testLID); n != 2 {
		t.Errorf("LID messages = %d, want 2", n)
	}
}

func TestLIDAuthorsRewrittenOnceMapped(t *testing.T) {
	s := setupChatStore(t)
	if err := s.StoreChat("123-456@g.us", "Group", time.Unix(100, 0)); err != nil {
		t.Fatal(err)
	}
	for id, sender := range map[string]string{"a": "999@lid", "b": "999", "c": "555", "d": "777@lid"} {
		if err := s.StoreMessage(id, "123-456@g.us", sender, "x", time.Unix(100, 0), false, "", "", "", nil, nil, nil, 0); err != nil {
			t.Fatal(err)
		}
	}

	migrateLIDChats(lidClient(map[string]string{"999": "222"}), s, waLog.Noop)

	for id, want := range map[string]string{"a": "222", "b": "222", "c": "555", "d": "777@lid"} {
		var got string
		if err := s.db.QueryRow("SELECT sender FROM messages WHERE id = ?", id).Scan(&got); err != nil || got != want {
			t.Errorf("sender of %s = %q, %v; want %q", id, got, err, want)
		}
	}
}

func TestSenderKeyKeepsUnresolvedLID(t *testing.T) {
	client := lidClient(map[string]string{"999": "222"})
	for in, want := range map[types.JID]string{
		{User: "999", Server: types.HiddenUserServer, Device: 3}: "222",
		{User: "777", Server: types.HiddenUserServer, Device: 3}: "777@lid",
		types.NewJID("333", types.DefaultUserServer):             "333",
	} {
		if got := senderKey(client, in); got != want {
			t.Errorf("senderKey(%s) = %q, want %q", in, got, want)
		}
		back, err := parseSender(want)
		if err != nil || back.User != want[:3] {
			t.Errorf("parseSender(%q) = %v, %v", want, back, err)
		}
	}
	if jid := pollCreatorJID(&whatsmeow.Client{Store: &store.Device{}}, "777@lid", false); jid.String() != "777@lid" {
		t.Errorf("poll creator = %s, want the stored LID", jid)
	}
}

func assertChat(t *testing.T, s *MessageStore, jid, name string, last time.Time) {
	t.Helper()
	var gotName string
	var gotTime time.Time
	if err := s.db.QueryRow("SELECT name, last_message_time FROM chats WHERE jid = ?", jid).Scan(&gotName, &gotTime); err != nil {
		t.Fatal(err)
	}
	if gotName != name || !gotTime.Equal(last) {
		t.Errorf("chat %s = %q %v, want %q %v", jid, gotName, gotTime, name, last)
	}
}
