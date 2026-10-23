package main

import (
	"net/url"
	"os"
	"path/filepath"
)

func storePath(parts ...string) string {
	root := os.Getenv("WHATSAPP_STORE_DIR")
	if root == "" {
		root = "store"
	}
	return filepath.Join(append([]string{root}, parts...)...)
}

func storeDSN(name, query string) string {
	path := &url.URL{Path: filepath.ToSlash(storePath(name))}
	return "file:" + path.EscapedPath() + "?" + query
}
