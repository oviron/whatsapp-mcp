package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMediaRootsNarrowDefaults(t *testing.T) {
	home, _, _, _ := mediaFixture(t)
	for _, name := range []string{"Documents/report.pdf", "projects/source.txt", "file.txt"} {
		path := mediaFile(t, filepath.Join(home, filepath.FromSlash(name)))
		expectMediaDenied(t, path)
		t.Setenv("WHATSAPP_MEDIA_DIRS", home)
		if _, err := readMediaFile(path); err != nil {
			t.Fatalf("explicit home root: %v", err)
		}
		t.Setenv("WHATSAPP_MEDIA_DIRS", "")
	}
}

func TestMediaRootsTempInsideExcludedDirectory(t *testing.T) {
	home, _, _, _ := mediaFixture(t)
	for _, zone := range []string{"AppData", "Library", ".cache"} {
		temp := filepath.Join(home, zone, "Local", "Temp")
		file := mediaFile(t, filepath.Join(temp, "tts.ogg"))
		t.Setenv("TMPDIR", temp)
		t.Setenv("TMP", temp)
		for _, roots := range []string{"", temp} {
			t.Setenv("WHATSAPP_MEDIA_DIRS", roots)
			if data, err := readMediaFile(file); err != nil || string(data) != "media" {
				t.Errorf("temp under %s, roots %q: %q, %v", zone, roots, data, err)
			}
		}
		t.Setenv("WHATSAPP_MEDIA_DIRS", home)
		expectMediaDenied(t, mediaFile(t, filepath.Join(home, zone, "secret")))
	}
}

func TestMediaRootsCaseAliases(t *testing.T) {
	home, _, _, outside := mediaFixture(t)
	homeAlias := filepath.Join(filepath.Dir(home), "HOME")
	if info, err := os.Stat(homeAlias); err != nil {
		t.Skip("case-sensitive filesystem")
	} else if original, err := os.Stat(home); err != nil || !os.SameFile(original, info) {
		t.Skip("case-sensitive filesystem")
	}
	store := filepath.Join(home, "Bridge", "store")
	download := mediaFile(t, filepath.Join(store, "chat", "voice.ogg"))
	t.Chdir(filepath.Dir(store))
	for _, path := range []string{
		filepath.Join(home, "Library", "secret"),
		filepath.Join(home, "AppData", "secret"),
		filepath.Join(home, ".ssh", "key"),
		filepath.Join(store, "whatsapp.db"),
	} {
		mediaFile(t, path)
	}
	for _, root := range []string{home, homeAlias} {
		t.Setenv("WHATSAPP_MEDIA_DIRS", root)
		for _, path := range []string{
			filepath.Join(home, "library", "secret"),
			filepath.Join(home, "appdata", "secret"),
			filepath.Join(homeAlias, ".ssh", "key"),
			filepath.Join(homeAlias, "bridge", "STORE", "whatsapp.db"),
		} {
			t.Run(filepath.Base(root)+"/"+filepath.Base(filepath.Dir(path)), func(t *testing.T) {
				expectMediaDenied(t, path)
			})
		}
	}
	for _, root := range []string{homeAlias, filepath.Join(homeAlias, "bridge", "STORE")} {
		t.Setenv("WHATSAPP_MEDIA_DIRS", root)
		if _, err := readMediaFile(download); err != nil {
			t.Errorf("case alias root %q: %v", root, err)
		}
	}
	mediaFile(t, filepath.Join(outside, "Uploads", "file.pdf"))
	t.Setenv("WHATSAPP_MEDIA_DIRS", filepath.Join(outside, "UPLOADS"))
	if _, err := readMediaFile(filepath.Join(outside, "uploads", "file.pdf")); err != nil {
		t.Errorf("configured root case alias: %v", err)
	}
}

func TestMediaRootsDistinctCaseDirectories(t *testing.T) {
	_, _, _, outside := mediaFixture(t)
	root := filepath.Join(outside, "Uploads")
	mediaFile(t, filepath.Join(root, "file.pdf"))
	alias := filepath.Join(outside, "uploads")
	if _, err := os.Stat(alias); err == nil {
		t.Skip("case-insensitive filesystem")
	}
	t.Setenv("WHATSAPP_MEDIA_DIRS", root)
	expectMediaDenied(t, mediaFile(t, filepath.Join(alias, "secret")))
}
