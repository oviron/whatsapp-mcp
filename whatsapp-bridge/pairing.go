package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"time"
)

// bridgeOptions are the command-line switches; everything else is configured through the environment.
type bridgeOptions struct {
	pairPhone string
	logout    bool
}

func parseBridgeOptions(args []string) (bridgeOptions, error) {
	fs := flag.NewFlagSet("whatsapp-bridge", flag.ContinueOnError)
	var o bridgeOptions
	fs.StringVar(&o.pairPhone, "pair-phone", os.Getenv("WHATSAPP_PAIR_PHONE"),
		"link with a code typed on the phone instead of scanning a QR code: the phone number in international format (also WHATSAPP_PAIR_PHONE)")
	fs.BoolVar(&o.logout, "logout", false,
		"unlink this bridge from WhatsApp, remove the session from store/whatsapp.db and exit; stop the running bridge first")
	return o, fs.Parse(args)
}

// pairingPrompt decides, for each QR event, what to show. With a phone number the first event asks
// for a pairing code (it needs the live login socket that event marks) and later QR codes stay
// hidden; if the request fails, pairing falls back to QR codes.
type pairingPrompt struct {
	phone     string
	request   func(phone string) (string, error)
	codeShown bool
}

func (p *pairingPrompt) onQR() (code string, showQR bool, err error) {
	if p.codeShown {
		return "", false, nil
	}
	if p.phone == "" {
		return "", true, nil
	}
	code, err = p.request(p.phone)
	if err != nil {
		p.phone = ""
		return "", true, err
	}
	p.codeShown = true
	return code, false, nil
}

type logoutClient interface {
	IsPaired() bool
	Connect() error
	WaitForConnection(time.Duration) bool
	Logout(context.Context) error
}

var errLogoutOffline = errors.New("could not reach WhatsApp to unlink this device")

// logoutBridge unlinks the device on the phone's side too, which deleting store/whatsapp.db alone
// does not: the phone would keep listing a dead linked device.
func logoutBridge(c logoutClient) (bool, error) {
	if !c.IsPaired() {
		return false, nil
	}
	if err := c.Connect(); err != nil {
		return false, err
	}
	if !c.WaitForConnection(30 * time.Second) {
		return false, errLogoutOffline
	}
	return true, c.Logout(context.Background())
}
