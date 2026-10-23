package main

import (
	"testing"
	"time"
)

func TestQuotedReply(t *testing.T) {
	store := setupChatStore(t)
	ts := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	group, dm, lidDM, status := "120363000000000000@g.us", "5511999990000@s.whatsapp.net", "987654321@lid", "status@broadcast"
	for _, chat := range []string{group, dm, lidDM, status} {
		if err := store.StoreChat(chat, "chat", ts); err != nil {
			t.Fatal(err)
		}
	}
	// Senders are stored by user part; only the senders table keeps the server.
	for _, jid := range []string{"5511999990000@s.whatsapp.net", "123456789@lid"} {
		if err := store.StoreSender(jid, "", "", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range []struct {
		id, chat, sender, content, media string
		fromMe                           bool
	}{
		{"pn", group, "5511999990000", "Please send the report", "", false},
		{"lid", group, "123456789", "From a hidden number", "", false},
		{"unknown", group, "555", "Sender never seen live", "", false},
		{"full", group, "444@lid", "Sender stored as a full JID", "", false},
		{"mine", group, "5511888880000", "My own message", "", true},
		{"direct", dm, "5511999990000", "Direct message", "", false},
		{"outbound", lidDM, "5511888880000", "Sent to a LID", "", true},
		{"image", group, "5511999990000", "a caption", "image", false},
		{"deleted", group, "5511999990000", revokedContent, "", false},
		{"poll", group, "5511999990000", pollSearchText("Lunch?"), "", false},
		{"card", group, "5511888880000", "[contact] Maria +5511999990000", "", true},
		{"status", status, "5511999990000", "A status update", "", false},
		{"own status", status, "5511888880000", "My status update", "", true},
		{"full status", status, "777@lid", "A status from a full JID", "", false},
	} {
		if err := store.StoreMessage(m.id, m.chat, m.sender, m.content, ts, m.fromMe, m.media, "", "", nil, nil, nil, 0); err != nil {
			t.Fatal(err)
		}
	}

	msg, err := quotedReply(store, group, group, "pn", "On it")
	if err != nil {
		t.Fatal(err)
	}
	ctx := msg.GetExtendedTextMessage().GetContextInfo()
	if msg.GetExtendedTextMessage().GetText() != "On it" || ctx.GetStanzaID() != "pn" ||
		ctx.GetParticipant() != "5511999990000@s.whatsapp.net" || ctx.GetQuotedMessage().GetConversation() != "Please send the report" {
		t.Fatalf("reply = %v", msg)
	}

	for _, c := range []struct{ id, chat, callerChat, participant string }{
		{"lid", group, group, "123456789@lid"},
		{"full", group, group, "444@lid"},
		{"mine", group, group, "5511888880000@s.whatsapp.net"},
		{"direct", dm, dm, dm},
		{"outbound", "5511777770000@s.whatsapp.net", lidDM, "5511888880000@s.whatsapp.net"},
	} {
		msg, err := quotedReply(store, c.chat, c.callerChat, c.id, "ok")
		if got := msg.GetExtendedTextMessage().GetContextInfo().GetParticipant(); err != nil || got != c.participant {
			t.Errorf("%s: participant = %q, %v; want %q", c.id, got, err, c.participant)
		}
	}

	for _, c := range []struct{ name, chat, id string }{
		{"other chat", dm, "pn"},
		{"missing", group, "nope"},
		{"media", group, "image"},
		{"deleted", group, "deleted"},
		{"poll", group, "poll"},
		{"contact card", group, "card"},
		{"status", status, "status"},
		{"own status", status, "own status"},
		{"status from a full JID", status, "full status"},
		{"sender of unknown kind", group, "unknown"},
	} {
		if _, err := quotedReply(store, c.chat, c.chat, c.id, "x"); err == nil {
			t.Errorf("%s: quoted a message it should refuse", c.name)
		}
	}
	if _, err := quotedReply(nil, group, group, "pn", "x"); err == nil {
		t.Error("nil store: no error")
	}
}
