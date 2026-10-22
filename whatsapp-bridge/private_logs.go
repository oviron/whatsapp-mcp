package main

import (
	"fmt"
	"regexp"
	"time"

	"go.mau.fi/whatsmeow"
	waBinary "go.mau.fi/whatsmeow/binary"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

func logMessage(timestamp time.Time, fromMe bool, chat, sender, mediaType, content string) {
	direction := "in"
	if fromMe {
		direction = "out"
	}
	if mediaType == "" {
		mediaType = "text"
	}
	line := fmt.Sprintf("[%s] direction=%s chat=%q sender=%q type=%q length=%d", timestamp.Format(time.RFC3339), direction, chat, sender, mediaType, len(content))
	if envBool("WHATSAPP_LOG_CONTENT") {
		line += fmt.Sprintf(" content=%q", content)
	}
	fmt.Println(line)
}

func logMediaUpload(resp whatsmeow.UploadResponse) {
	fmt.Printf("Media uploaded: bytes=%d\n", resp.FileLength)
}

var privateURLPattern = regexp.MustCompile(`https?://\S+`)

// Protocol payloads can carry plaintext and keys even at INFO level.
type privateLibraryLogger struct{ waLog.Logger }

func privateLibraryMessage(msg string, args []any) string {
	safe := make([]any, len(args))
	for i, arg := range args {
		switch arg.(type) {
		case []byte, waBinary.Node, *waBinary.Node, proto.Message:
			safe[i] = fmt.Sprintf("<%T>", arg)
		default:
			safe[i] = arg
		}
	}
	return privateURLPattern.ReplaceAllString(fmt.Sprintf(msg, safe...), "<url>")
}

func (l privateLibraryLogger) Errorf(msg string, args ...any) {
	l.Logger.Errorf("%s", privateLibraryMessage(msg, args))
}
func (l privateLibraryLogger) Warnf(msg string, args ...any) {
	l.Logger.Warnf("%s", privateLibraryMessage(msg, args))
}
func (l privateLibraryLogger) Infof(msg string, args ...any) {
	l.Logger.Infof("%s", privateLibraryMessage(msg, args))
}
func (l privateLibraryLogger) Debugf(_ string, _ ...any) {}
func (l privateLibraryLogger) Sub(module string) waLog.Logger {
	return privateLibraryLogger{l.Logger.Sub(module)}
}
