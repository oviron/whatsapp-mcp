//go:build sqlite_fts5 || fts5

package main

import (
	"reflect"
	"testing"
)

func TestBatchMessageContextWithFTS(t *testing.T) {
	s := setupChatStore(t)
	chat := "alpha@g.us"
	if err := s.StoreChat(chat, chat, ts("2026-10-01T06:00:00Z")); err != nil {
		t.Fatal(err)
	}
	for _, m := range []struct{ id, at string }{
		{"before", "2026-10-01T07:00:00Z"}, {"hit", "2026-10-01T08:00:00Z"},
		{"after", "2026-10-01T09:00:00Z"}, {"later", "2026-10-01T10:00:00Z"},
	} {
		readMessage(t, s.db, chat, m.id, "111", m.at, false)
	}
	mustExec(t, s, `UPDATE messages SET content = 'gamma beta alpha' WHERE id = 'hit'`)
	waitForFTS(t, s.db)
	query := "alpha gamma" // matches only through FTS: the words are not adjacent
	got, err := listMessages(s.db, MessagesRequest{Query: &query, IncludeContext: true, ContextBefore: 1, ContextAfter: 1})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"before", "hit", "after"}; !reflect.DeepEqual(readIDs(got.Messages), want) {
		t.Fatalf("messages = %v, want %v", readIDs(got.Messages), want)
	}
}
