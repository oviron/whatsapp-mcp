package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

var mediaLocks = struct {
	sync.Mutex
	entries map[string]*mediaLock
}{entries: make(map[string]*mediaLock)}

type mediaLock struct {
	sync.Mutex
	users int
}

func lockMediaMessage(chatJID, id string) func() {
	key := mediaMessageKey(chatJID, id)
	mediaLocks.Lock()
	entry := mediaLocks.entries[key]
	if entry == nil {
		entry = &mediaLock{}
		mediaLocks.entries[key] = entry
	}
	entry.users++
	mediaLocks.Unlock()
	entry.Lock()
	return func() {
		entry.Unlock()
		mediaLocks.Lock()
		entry.users--
		if entry.users == 0 {
			delete(mediaLocks.entries, key)
		}
		mediaLocks.Unlock()
	}
}

func validMediaFile(path string, length uint64, hash []byte) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || (length != 0 && uint64(info.Size()) != length) {
		return false
	}
	if len(hash) == 0 {
		return true
	}
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return false
	}
	return bytes.Equal(h.Sum(nil), hash)
}

func writeMediaFile(path string, length uint64, hash []byte, download func(*os.File) error) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".media-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := download(file); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if !validMediaFile(file.Name(), length, hash) {
		return fmt.Errorf("downloaded media failed size or SHA-256 validation")
	}
	return os.Rename(file.Name(), path)
}
