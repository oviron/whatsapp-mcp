package main

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// quotedReply builds text that quotes a stored text message, so WhatsApp shows
// it as a native reply. The chat is looked up under its LID-to-phone resolved
// JID and under the one the caller used, since an outbound text is stored under
// the latter. Only text is quoted: a media message would need its own media
// payload in the quote.
func quotedReply(store *MessageStore, chat, callerChat, messageID, text string) (*waE2E.Message, error) {
	if store == nil {
		return nil, errors.New("message store unavailable")
	}
	var sender, content, mediaType string
	var fromMe bool
	err := store.db.QueryRow(`SELECT chat_jid, COALESCE(sender, ''), COALESCE(content, ''), COALESCE(media_type, ''), COALESCE(is_from_me, 0)
		FROM messages WHERE id = ? AND chat_jid IN (?, ?)`, messageID, chat, callerChat).Scan(&chat, &sender, &content, &mediaType, &fromMe)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("reply target not found in this chat")
	}
	if err != nil {
		fmt.Printf("Failed to read reply target: %v\n", err)
		return nil, errors.New("could not read the reply target")
	}
	if sender == "" || content == "" || mediaType != "" || bridgeMarker(content) {
		return nil, errors.New("reply target must be a text message")
	}
	if sender, err = quotedSender(store, chat, sender, fromMe); err != nil {
		return nil, err
	}
	return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text: proto.String(text),
		ContextInfo: &waE2E.ContextInfo{
			StanzaID:      proto.String(messageID),
			Participant:   proto.String(sender),
			QuotedMessage: &waE2E.Message{Conversation: proto.String(content)},
		},
	}}, nil
}

// quotedSender turns a stored sender into a JID. Senders are mostly stored as
// the user part only, which does not say whether it is a phone number or an
// unresolved LID, so guessing a server would attribute the quote to someone else.
func quotedSender(store *MessageStore, chat, sender string, fromMe bool) (string, error) {
	direct := strings.HasSuffix(chat, "@"+types.DefaultUserServer) || strings.HasSuffix(chat, "@"+types.HiddenUserServer)
	if !direct && !strings.HasSuffix(chat, "@"+types.GroupServer) {
		return "", errors.New("replies are supported in direct and group chats only")
	}
	switch {
	case strings.Contains(sender, "@"):
		return sender, nil
	case fromMe:
		// Own messages are stored under the phone number of this account.
		return sender + "@" + types.DefaultUserServer, nil
	case direct:
		// In a direct chat the other person is the chat itself.
		return chat, nil
	}
	err := store.db.QueryRow(`SELECT jid FROM senders WHERE jid IN (?, ?)`,
		sender+"@"+types.DefaultUserServer, sender+"@"+types.HiddenUserServer).Scan(&sender)
	if err != nil {
		return "", errors.New("cannot tell who wrote the reply target")
	}
	return sender, nil
}

// bridgeMarker reports content the bridge stored in place of message text
// (deleted messages, polls, contact cards), which must not be quoted as text.
func bridgeMarker(content string) bool {
	return content == revokedContent || strings.HasPrefix(content, pollSearchText("")) || strings.HasPrefix(content, "[contact] ")
}
