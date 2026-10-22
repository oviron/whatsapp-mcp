package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func mediaFixture(t *testing.T) (home, temp, store, outside string) {
	t.Helper()
	base := t.TempDir()
	home = filepath.Join(base, "home")
	temp = filepath.Join(base, "temp")
	store = filepath.Join(home, ".local", "whatsapp-mcp", "store")
	outside = filepath.Join(base, "outside")
	for _, dir := range []string{home, temp, store, outside} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("TMPDIR", temp)
	t.Setenv("TMP", temp)
	t.Setenv("WHATSAPP_MEDIA_DIRS", "")
	t.Chdir(filepath.Dir(store))
	return
}

func mediaFile(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("media"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func expectMediaDenied(t *testing.T, path string) {
	t.Helper()
	data, err := readMediaFile(path)
	if err == nil || data != nil {
		t.Fatalf("readMediaFile(%q) = %q, %v; want rejection without data", path, data, err)
	}
	if strings.Contains(err.Error(), path) || strings.Contains(err.Error(), os.Getenv("HOME")) {
		t.Fatalf("error leaks a filesystem path: %v", err)
	}
}

func TestMediaPathDefaultRoots(t *testing.T) {
	home, temp, store, outside := mediaFixture(t)
	for _, path := range []string{
		filepath.Join(home, "Downloads", "report.pdf"),
		filepath.Join(temp, "tts.ogg"),
		filepath.Join(store, "chat", "message.ogg"),
	} {
		path = mediaFile(t, path)
		data, err := readMediaFile(path)
		if err != nil || string(data) != "media" {
			t.Errorf("readMediaFile(%q) = %q, %v", path, data, err)
		}
	}
	for _, path := range []string{
		filepath.Join(outside, "secret"),
		filepath.Join(home, ".ssh", "id_ed25519"),
		filepath.Join(home, ".aws", "credentials"),
		filepath.Join(home, ".config", "app", "token"),
		filepath.Join(home, ".env"),
		filepath.Join(home, "Library", "Keychains", "login.keychain-db"),
		filepath.Join(home, "AppData", "Roaming", "app", "cookies"),
		filepath.Join(store, "whatsapp.db"),
		filepath.Join(store, "messages.db-wal"),
		filepath.Join(store, "messages.db-shm"),
		filepath.Join(store, "session-backup"),
	} {
		expectMediaDenied(t, mediaFile(t, path))
	}
}

func TestMediaPathConfiguredRoots(t *testing.T) {
	home, temp, store, outside := mediaFixture(t)
	allowed := filepath.Join(outside, "uploads")
	file := mediaFile(t, filepath.Join(allowed, "file.pdf"))
	second := mediaFile(t, filepath.Join(outside, "second", "file.pdf"))
	t.Setenv("WHATSAPP_MEDIA_DIRS", allowed+string(os.PathListSeparator)+filepath.Dir(second))
	for _, path := range []string{file, second} {
		if _, err := readMediaFile(path); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{
		filepath.Join(home, "file.pdf"), filepath.Join(temp, "file.ogg"),
		filepath.Join(store, "chat", "file.ogg"), filepath.Join(allowed+"-other", "file.pdf"),
	} {
		expectMediaDenied(t, mediaFile(t, path))
	}
	t.Setenv("WHATSAPP_MEDIA_DIRS", home+string(os.PathListSeparator)+store)
	expectMediaDenied(t, mediaFile(t, filepath.Join(home, ".ssh", "key")))
	expectMediaDenied(t, mediaFile(t, filepath.Join(store, "whatsapp.db")))
	t.Setenv("WHATSAPP_MEDIA_DIRS", filepath.Join(outside, "missing"))
	expectMediaDenied(t, file)
}

func TestMediaPathSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires Developer Mode or administrator rights")
	}
	home, _, store, outside := mediaFixture(t)
	allowed := mediaFile(t, filepath.Join(home, "Downloads", "file.pdf"))
	for name, target := range map[string]string{
		"outside":  mediaFile(t, filepath.Join(outside, "secret")),
		"hidden":   mediaFile(t, filepath.Join(home, ".ssh", "key")),
		"database": mediaFile(t, filepath.Join(store, "whatsapp.db")),
	} {
		alias := filepath.Join(home, "Downloads", name)
		if err := os.Symlink(target, alias); err != nil {
			t.Fatal(err)
		}
		expectMediaDenied(t, alias)
	}
	alias := filepath.Join(home, "Downloads", "alias.pdf")
	if err := os.Symlink(allowed, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := readMediaFile(alias); err != nil {
		t.Fatal(err)
	}
	rootAlias := filepath.Join(outside, "uploads")
	if err := os.Symlink(filepath.Dir(allowed), rootAlias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WHATSAPP_MEDIA_DIRS", rootAlias)
	if _, err := readMediaFile(filepath.Join(rootAlias, "file.pdf")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, ".ssh"), filepath.Join(home, "keys")); err != nil {
		t.Fatal(err)
	}
	expectMediaDenied(t, filepath.Join(home, "keys", "key"))
}

func TestMediaPathRegularFileAndLimit(t *testing.T) {
	home, _, _, _ := mediaFixture(t)
	expectMediaDenied(t, home)
	expectMediaDenied(t, filepath.Join(home, "missing"))
	large := mediaFile(t, filepath.Join(home, "Downloads", "large.pdf"))
	f, err := os.Create(large)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(100*1024*1024 + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	expectMediaDenied(t, large)
	if runtime.GOOS != "windows" {
		fifo := filepath.Join(home, "Downloads", "fifo")
		if err := exec.Command("mkfifo", fifo).Run(); err != nil {
			t.Fatal(err)
		}
		expectMediaDenied(t, fifo)
	}
}

func TestMediaPathLimitInclusive(t *testing.T) {
	home, _, _, _ := mediaFixture(t)
	path := mediaFile(t, filepath.Join(home, "Downloads", "at-limit.pdf"))
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(100 * 1024 * 1024); err != nil {
		t.Fatal(err)
	}
	file.Close()
	data, err := readMediaFile(path)
	if err != nil || len(data) != 100*1024*1024 {
		t.Fatalf("file at limit: len=%d, err=%v", len(data), err)
	}
}

func TestMediaPathReplacementAfterResolution(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires Developer Mode or administrator rights")
	}
	home, _, _, outside := mediaFixture(t)
	secret := mediaFile(t, filepath.Join(outside, "secret"))
	resolved := mediaFile(t, filepath.Join(home, "Downloads", "file"))
	if err := os.Remove(resolved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, resolved); err != nil {
		t.Fatal(err)
	}
	if file, err := openResolvedMedia(resolved); err == nil {
		file.Close()
		t.Fatal("opened a file replaced with a symlink after resolution")
	}
	if err := os.RemoveAll(filepath.Dir(resolved)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Dir(resolved)); err != nil {
		t.Fatal(err)
	}
	if file, err := openResolvedMedia(filepath.Join(home, "Downloads", "secret")); err == nil {
		file.Close()
		t.Fatal("opened a directory replaced with a symlink after resolution")
	}
}
