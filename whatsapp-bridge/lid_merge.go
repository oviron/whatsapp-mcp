package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// senderKey is what messages.sender stores for an author: the phone number when whatsmeow knows
// the LID's number, otherwise the whole LID JID. A bare LID user would later read as a phone
// number (a media retry would address <lid>@s.whatsapp.net).
func senderKey(client *whatsmeow.Client, jid types.JID) string {
	pn := resolveToPN(client, jid)
	if pn.Server == types.HiddenUserServer {
		return pn.ToNonAD().String()
	}
	return pn.User
}

// parseSender turns a stored messages.sender back into a JID: a full JID as stored, a bare user as
// a phone number.
func parseSender(sender string) (types.JID, error) {
	if strings.Contains(sender, "@") {
		return types.ParseJID(sender)
	}
	return types.JID{User: sender, Server: types.DefaultUserServer}, nil
}

// migrateLIDChats merges chats stored under a LID JID into the phone-number chat, and rewrites
// authors stored as LIDs, once whatsmeow knows the mapping. Each chat merges in its own
// transaction: any failure rolls that chat back whole and leaves it for the next start.
func migrateLIDChats(client *whatsmeow.Client, store *MessageStore, logger waLog.Logger) {
	if client == nil || store == nil || store.db == nil {
		return
	}
	lidChats, err := queryStrings(store.db, "SELECT jid FROM chats WHERE jid LIKE '%@"+types.HiddenUserServer+"'")
	if err != nil {
		logger.Warnf("LID migration: failed to list LID chats: %v", err)
		return
	}
	merged, unmapped, failed := 0, 0, 0
	for _, lid := range lidChats {
		lidJID, err := types.ParseJID(lid)
		pn := resolveToPN(client, lidJID)
		if err != nil || pn.Server != types.DefaultUserServer {
			unmapped++
			continue
		}
		if err := mergeLIDChat(store.db, lid, pn.String()); err != nil {
			failed++
			logger.Warnf("LID migration: %s left as is: %v", lid, err)
			continue
		}
		merged++
	}
	// Media moves after the chat's rows: a crash in between leaves a LID directory whose chat is
	// gone, which this picks up on the next start.
	lidDirs, _ := filepath.Glob(filepath.Join("store", "*@"+types.HiddenUserServer))
	for _, dir := range lidDirs {
		lid := filepath.Base(dir)
		lidJID, err := types.ParseJID(lid)
		pn := resolveToPN(client, lidJID)
		if err != nil || pn.Server != types.DefaultUserServer || chatExists(store.db, lid) {
			continue
		}
		if err := moveChatMedia(store.db, lid, pn.String()); err != nil {
			logger.Warnf("LID migration: media of %s not fully moved: %v", lid, err)
		}
	}
	senders, err := migrateLIDSenders(client, store.db)
	if err != nil {
		logger.Warnf("LID migration: failed to rewrite LID authors: %v", err)
	}
	if merged+unmapped+failed+senders > 0 {
		logger.Infof("LID migration: %d chat(s) merged, %d without a known number yet, %d failed; %d LID author(s) rewritten",
			merged, unmapped, failed, senders)
	}
}

// mergeLIDChat moves everything stored under the LID chat to the phone-number chat. A message
// present under both keeps the phone-number row and fills its empty fields from the LID copy, so
// a transcription that only the LID copy has survives.
func mergeLIDChat(db *sql.DB, lid, pn string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	steps := []string{
		`INSERT INTO chats (jid, name, last_message_time)
			SELECT ?2, name, last_message_time FROM chats WHERE jid = ?1
			ON CONFLICT(jid) DO UPDATE SET
				name = COALESCE(NULLIF(chats.name, ''), excluded.name),
				last_message_time = CASE
					WHEN julianday(chats.last_message_time) IS NULL
						OR julianday(excluded.last_message_time) > julianday(chats.last_message_time)
					THEN excluded.last_message_time ELSE chats.last_message_time END`,
		`UPDATE messages SET
				sender = COALESCE(NULLIF(messages.sender, ''), l.sender),
				content = COALESCE(NULLIF(messages.content, ''), l.content),
				media_type = COALESCE(NULLIF(messages.media_type, ''), l.media_type),
				filename = COALESCE(NULLIF(messages.filename, ''), l.filename),
				url = COALESCE(NULLIF(messages.url, ''), l.url),
				media_key = COALESCE(NULLIF(messages.media_key, x''), l.media_key),
				file_sha256 = COALESCE(NULLIF(messages.file_sha256, x''), l.file_sha256),
				file_enc_sha256 = COALESCE(NULLIF(messages.file_enc_sha256, x''), l.file_enc_sha256),
				file_length = COALESCE(NULLIF(messages.file_length, 0), l.file_length)
			FROM (SELECT * FROM messages WHERE chat_jid = ?1) AS l
			WHERE messages.chat_jid = ?2 AND messages.id = l.id`,
		`UPDATE OR IGNORE messages SET chat_jid = ?2 WHERE chat_jid = ?1`,
		`DELETE FROM messages WHERE chat_jid = ?1`,
		`UPDATE OR IGNORE message_mentions SET chat_jid = ?2 WHERE chat_jid = ?1`,
		`DELETE FROM message_mentions WHERE chat_jid = ?1`,
		`UPDATE OR IGNORE polls SET chat_jid = ?2 WHERE chat_jid = ?1`,
		`DELETE FROM polls WHERE chat_jid = ?1`,
		// A voter's newer vote wins, whichever key it was stored under.
		`DELETE FROM poll_votes AS p WHERE chat_jid = ?2 AND EXISTS (SELECT 1 FROM poll_votes l
				WHERE l.chat_jid = ?1 AND l.poll_id = p.poll_id AND l.voter = p.voter
				AND julianday(l.timestamp) > julianday(p.timestamp))`,
		`UPDATE OR IGNORE poll_votes SET chat_jid = ?2 WHERE chat_jid = ?1`,
		`DELETE FROM poll_votes WHERE chat_jid = ?1`,
		`UPDATE OR IGNORE favorites SET chat_jid = ?2 WHERE chat_jid = ?1`, // a decision on the number wins
		`DELETE FROM favorites WHERE chat_jid = ?1`,
		`UPDATE matters SET chat_jid = ?2 WHERE chat_jid = ?1`,
		`UPDATE drafts SET chat_jid = ?2 WHERE chat_jid = ?1`,
		`DELETE FROM chats WHERE jid = ?1`,
	}
	for _, q := range steps {
		if _, err := tx.Exec(q, lid, pn); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// moveChatMedia moves downloaded media from the LID chat's directory to the phone-number one, so
// the cache (often the only copy once the CDN expired) stays where downloadMedia looks: under
// <id>_<filename> of the merged row, whose filename may come from the phone-number copy.
func moveChatMedia(db *sql.DB, lid, pn string) error {
	from := filepath.Join("store", strings.ReplaceAll(lid, ":", "_"))
	to := filepath.Join("store", strings.ReplaceAll(pn, ":", "_"))
	entries, err := os.ReadDir(from)
	if os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if err := os.MkdirAll(to, 0755); err != nil {
		return err
	}
	var kept int
	for _, e := range entries {
		dst := filepath.Join(to, mergedMediaName(db, pn, e.Name()))
		if _, err := os.Lstat(dst); err == nil {
			kept++
			continue
		}
		if err := os.Rename(filepath.Join(from, e.Name()), dst); err != nil {
			return err
		}
	}
	if kept > 0 {
		return fmt.Errorf("%d file(s) already present under %s, left in %s", kept, to, from)
	}
	return os.Remove(from)
}

// mergedMediaName is the cache name for a moved file: <id>_<filename> of the row it belongs to in
// the merged chat, or the name as is when no row claims it.
func mergedMediaName(db *sql.DB, chat, name string) string {
	for i := strings.Index(name, "_"); i > 0; i = nextUnderscore(name, i) {
		var filename string
		if db.QueryRow(`SELECT filename FROM messages WHERE chat_jid = ? AND id = ?`, chat, name[:i]).Scan(&filename) == nil {
			if path, err := safeMediaPath(".", name[:i], filename); err == nil {
				return filepath.Base(path)
			}
			return name
		}
	}
	return name
}

func nextUnderscore(s string, after int) int {
	if j := strings.Index(s[after+1:], "_"); j >= 0 {
		return after + 1 + j
	}
	return -1
}

func chatExists(db *sql.DB, jid string) bool {
	var n int
	return db.QueryRow(`SELECT COUNT(*) FROM chats WHERE jid = ?`, jid).Scan(&n) == nil && n > 0
}

// migrateLIDSenders rewrites authors and poll voters stored as a LID (whole JID, or a bare user
// from before senderKey) to the phone number once the mapping is known.
func migrateLIDSenders(client *whatsmeow.Client, db *sql.DB) (int, error) {
	if client == nil || client.Store == nil || client.Store.LIDs == nil {
		return 0, nil
	}
	lidLike := `LIKE '%@` + types.HiddenUserServer + `'`
	senders, err := queryStrings(db, `SELECT sender FROM messages WHERE sender != '' AND (sender `+lidLike+` OR sender NOT LIKE '%@%')
		UNION SELECT voter FROM poll_votes WHERE voter != '' AND (voter `+lidLike+` OR voter NOT LIKE '%@%')`)
	if err != nil {
		return 0, err
	}
	rewritten := 0
	for _, s := range senders {
		lid := types.JID{User: strings.TrimSuffix(s, "@"+types.HiddenUserServer), Server: types.HiddenUserServer}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		pn, err := client.Store.LIDs.GetPNForLID(ctx, lid)
		cancel()
		if err != nil || pn.IsEmpty() {
			continue
		}
		if err := rewriteLIDAuthor(db, s, pn.User); err != nil {
			return rewritten, err
		}
		rewritten++
	}
	return rewritten, nil
}

// rewriteLIDAuthor moves one author to the phone number; of two votes on one poll, the newer stays.
func rewriteLIDAuthor(db *sql.DB, from, to string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`UPDATE messages SET sender = ?2 WHERE sender = ?1`,
		`DELETE FROM poll_votes AS o WHERE voter = ?1 AND EXISTS (SELECT 1 FROM poll_votes n
			WHERE n.poll_id = o.poll_id AND n.chat_jid = o.chat_jid AND n.voter = ?2
			AND julianday(n.timestamp) >= julianday(o.timestamp))`,
		`UPDATE OR REPLACE poll_votes SET voter = ?2 WHERE voter = ?1`,
	} {
		if _, err := tx.Exec(q, from, to); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func queryStrings(db *sql.DB, query string, args ...any) ([]string, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
