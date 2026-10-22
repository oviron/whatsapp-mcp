package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

const pairingQRPath = "store/whatsapp-qr.png"

func restrictPermissions(path string, mode fs.FileMode) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Mode().Perm() & ^mode == 0 {
		return nil
	}
	return os.Chmod(path, info.Mode().Perm()&mode)
}

func privateDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	return restrictPermissions(path, 0700)
}

func prepareStore() error {
	if err := privateDirectory("store"); err != nil {
		return err
	}
	return filepath.WalkDir("store", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return restrictPermissions(path, 0700)
		}
		if entry.Type().IsRegular() {
			return restrictPermissions(path, 0600)
		}
		return nil
	})
}

func privateFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := restrictPermissions(path, 0600); err != nil {
		return err
	}
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	return f.Close()
}

func prepareDatabase(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return restrictPermissions(path, 0600)
}

func updatePairingQR(png []byte) error {
	qrState.Lock()
	qrState.png = png
	qrState.connected = png == nil
	qrState.Unlock()
	if png != nil {
		return privateFile(pairingQRPath, png)
	}
	err := os.Remove(pairingQRPath)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
