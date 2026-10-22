package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
)

type messageActionClient interface {
	IsConnected() bool
	OwnID() *types.JID
	NormalizeChat(types.JID) types.JID
	BuildReaction(types.JID, types.JID, types.MessageID, string) *waProto.Message
	BuildRevoke(types.JID, types.JID, types.MessageID) *waProto.Message
	SendMessage(context.Context, types.JID, *waProto.Message, ...whatsmeow.SendRequestExtra) (whatsmeow.SendResponse, error)
}

type whatsmeowActions struct{ *whatsmeow.Client }

func (c whatsmeowActions) OwnID() *types.JID {
	if c.Client == nil || c.Store == nil {
		return nil
	}
	return c.Store.ID
}

func (c whatsmeowActions) NormalizeChat(chat types.JID) types.JID {
	return resolveToPN(c.Client, chat)
}

func groupMessageSender(store *MessageStore, chat types.JID, id string) (types.JID, error) {
	if store == nil {
		return types.EmptyJID, invalid("unknown group message sender: message store unavailable")
	}
	var sender sql.NullString
	err := store.db.QueryRow("SELECT sender FROM messages WHERE id = ? AND chat_jid = ?", id, chat.String()).Scan(&sender)
	if errors.Is(err, sql.ErrNoRows) {
		return types.EmptyJID, invalid("unknown group message sender: message not found in this chat")
	} else if err != nil {
		return types.EmptyJID, fmt.Errorf("failed to read group message sender: %w", err)
	}
	if sender.String == "" || sender.String == chat.User || sender.String == chat.String() {
		return types.EmptyJID, invalid("unknown group message sender: stored author is empty or identifies the group")
	}
	jid, err := parseSender(sender.String)
	if err != nil || jid.User == "" || (jid.Server != types.DefaultUserServer && jid.Server != types.HiddenUserServer) {
		return types.EmptyJID, invalid("invalid group message sender: expected a PN or LID author")
	}
	return jid.ToNonAD(), nil
}

// writeActionError answers 400 for a request the bridge cannot act on and 500 for a store failure.
func writeActionError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	var bad *errInvalidRequest
	if errors.As(err, &bad) {
		status = http.StatusBadRequest
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(MarkChatResponse{Success: false, Message: err.Error()})
}
