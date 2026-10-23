package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if err := os.Unsetenv("WHATSAPP_STORE_DIR"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func TestStoreDirIgnoresInheritedEnvironment(t *testing.T) {
	if os.Getenv("WHATSAPP_TEST_STORE_ISOLATION") == "1" {
		setupChatStore(t)
		return
	}
	external := t.TempDir()
	marker := filepath.Join(external, "keep")
	if err := os.WriteFile(marker, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestStoreDirIgnoresInheritedEnvironment$")
	cmd.Env = append(os.Environ(), "WHATSAPP_STORE_DIR="+external, "WHATSAPP_TEST_STORE_ISOLATION=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child test: %v\n%s", err, out)
	}
	entries, err := os.ReadDir(external)
	if err != nil || len(entries) != 1 || entries[0].Name() != "keep" {
		t.Fatalf("inherited store was touched: %v, %v", entries, err)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "untouched" {
		t.Fatalf("marker changed: %q, %v", data, err)
	}
}

func TestStoreDirRejectsRelativePath(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, root := range []string{"relative data", "~/literal", "../outside"} {
		t.Run(root, func(t *testing.T) {
			t.Setenv("WHATSAPP_STORE_DIR", root)
			s, err := NewMessageStore()
			if s != nil {
				s.Close()
			}
			if err == nil || !strings.Contains(err.Error(), "WHATSAPP_STORE_DIR must be an absolute path") {
				t.Fatalf("relative store accepted: %v", err)
			}
		})
	}
}

func TestStoreDirSweepPassesAbsolutePath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake Python executable")
	}
	if os.Getenv("WHATSAPP_TEST_SWEEP") == "1" {
		root := t.TempDir()
		bridge := filepath.Join(root, "whatsapp-bridge")
		bin := filepath.Join(root, "whatsapp-mcp-server", ".venv", "bin")
		for _, dir := range []string{bridge, bin} {
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
		}
		t.Chdir(bridge)
		t.Setenv("TMPDIR", root)
		probe := filepath.Join(root, "probe")
		t.Setenv("WHATSAPP_TEST_PROBE", probe)
		configured := os.Getenv("WHATSAPP_TEST_SWEEP_STORE")
		t.Setenv("WHATSAPP_STORE_DIR", configured)
		for path, data := range map[string]string{
			filepath.Join(bin, "python3"):                               "#!/bin/sh\nprintf '%s' \"$WHATSAPP_STORE_DIR\" > \"$WHATSAPP_TEST_PROBE\"\n",
			filepath.Join(root, "whatsapp-mcp-server", "transcribe.py"): "",
		} {
			if err := os.WriteFile(path, []byte(data), 0700); err != nil {
				t.Fatal(err)
			}
		}
		want := configured
		if want == "" {
			want = filepath.Join(bridge, "store")
		}
		startTranscriptionSweep(20 * time.Millisecond)
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if data, err := os.ReadFile(probe); err == nil && string(data) == want {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		data, err := os.ReadFile(probe)
		t.Fatalf("sweep store = %q, %v; want %q", data, err, want)
	}

	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, configured := range []string{"", filepath.Join(t.TempDir(), "external")} {
		t.Run(configured, func(t *testing.T) {
			cmd := exec.Command(binary, "-test.run=^TestStoreDirSweepPassesAbsolutePath$")
			cmd.Env = append(os.Environ(), "WHATSAPP_TEST_SWEEP=1", "WHATSAPP_TEST_SWEEP_STORE="+configured)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("child sweep: %v\n%s", err, out)
			}
		})
	}
}
