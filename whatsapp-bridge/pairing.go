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
