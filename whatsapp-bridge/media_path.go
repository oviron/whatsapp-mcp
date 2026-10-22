package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxMediaBytes = 100 * 1024 * 1024

var errMediaPath = errors.New("Media file unavailable or outside allowed directories (WHATSAPP_MEDIA_DIRS)")

func mediaRelative(root, path string) (string, bool) {
	rel, err := filepath.Rel(root, path)
	return rel, err == nil && filepath.IsLocal(rel)
}

func mediaPathBlocked(path, home, store string) bool {
	storeRel, inStore := mediaRelative(store, path)
	if inStore && !strings.ContainsRune(storeRel, filepath.Separator) {
		return true
	}
	if rel, ok := mediaRelative(home, path); ok && rel != "." {
		top, _, _ := strings.Cut(rel, string(filepath.Separator))
		// Dotfiles hold credentials; Library (macOS) and AppData (Windows) hold keychains,
		// browser cookies and other apps' message databases without a leading dot.
		if strings.HasPrefix(top, ".") || top == "Library" || top == "AppData" {
			// Downloads live below store/<chat>/ even when the install is in ~/.local.
			return !inStore
		}
	}
	return false
}

// Walk the resolved path using directory handles: a swapped symlink must never
// turn an already checked path into a different file or a blocking FIFO.
func openResolvedMedia(path string) (*os.File, error) {
	volumeRoot := filepath.VolumeName(path) + string(filepath.Separator)
	root, err := os.OpenRoot(volumeRoot)
	if err != nil {
		return nil, errMediaPath
	}
	defer func() { root.Close() }()
	parts := strings.Split(strings.TrimPrefix(path, volumeRoot), string(filepath.Separator))
	for _, part := range parts[:len(parts)-1] {
		info, err := root.Lstat(part)
		if err != nil || !info.IsDir() {
			return nil, errMediaPath
		}
		next, err := root.OpenRoot(part)
		if err != nil {
			return nil, errMediaPath
		}
		opened, err := next.Stat(".")
		if err != nil || !os.SameFile(info, opened) {
			next.Close()
			return nil, errMediaPath
		}
		root.Close()
		root = next
	}
	name := parts[len(parts)-1]
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errMediaPath
	}
	file, err := root.OpenFile(name, os.O_RDONLY|mediaOpenFlags, 0)
	if err != nil {
		return nil, errMediaPath
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		file.Close()
		return nil, errMediaPath
	}
	return file, nil
}

func readMediaFile(path string) ([]byte, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, errMediaPath
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, errMediaPath
	}
	home, err = filepath.EvalSymlinks(home)
	if err != nil {
		return nil, errMediaPath
	}
	store, err := filepath.Abs("store")
	if err != nil {
		return nil, errMediaPath
	}
	if mediaPathBlocked(abs, home, store) {
		return nil, errMediaPath
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, errMediaPath
	}
	if canonical, err := filepath.EvalSymlinks(store); err == nil {
		store = canonical
	}
	if mediaPathBlocked(resolved, home, store) {
		return nil, errMediaPath
	}
	roots := []string{home, os.TempDir(), store}
	if configured := os.Getenv("WHATSAPP_MEDIA_DIRS"); configured != "" {
		roots = filepath.SplitList(configured)
	}
	allowed := false
	for _, root := range roots {
		if root == "" {
			continue
		}
		root, err = filepath.Abs(root)
		if err != nil {
			continue
		}
		root, err = filepath.EvalSymlinks(root)
		if err == nil {
			if rel, ok := mediaRelative(root, resolved); ok && rel != "." {
				allowed = true
			}
		}
	}
	if !allowed {
		return nil, errMediaPath
	}
	file, err := openResolvedMedia(resolved)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("Media must be a regular file")
	}
	if info.Size() > maxMediaBytes {
		return nil, errors.New("Media exceeds the 100 MiB size limit")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxMediaBytes+1))
	if err != nil {
		return nil, errors.New("Unable to read media file")
	}
	if len(data) > maxMediaBytes {
		return nil, errors.New("Media exceeds the 100 MiB size limit")
	}
	return data, nil
}
