package main

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/mod/module"
	"golang.org/x/mod/zip"
)

const (
	modulePath = "github.com/war-and-code/dircue"
	version    = "v1.0.0"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: modulezip SOURCE_DIR PROXY_DIR")
		os.Exit(2)
	}
	moduleDir, proxyDir := os.Args[1], os.Args[2]
	versionDir := filepath.Join(proxyDir, modulePath, "@v")
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		fatal(err)
	}
	zipPath := filepath.Join(versionDir, version+".zip")
	file, err := os.Create(zipPath)
	if err != nil {
		fatal(err)
	}
	if err := zip.CreateFromDir(file, module.Version{Path: modulePath, Version: version}, moduleDir); err != nil {
		_ = file.Close()
		fatal(err)
	}
	if err := file.Close(); err != nil {
		fatal(err)
	}
	mod, err := os.ReadFile(filepath.Join(moduleDir, "go.mod"))
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versionDir, version+".mod"), mod, 0o644); err != nil {
		fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versionDir, version+".info"), []byte("{\"Version\":\""+version+"\",\"Time\":\"2026-09-23T00:00:00Z\"}\n"), 0o644); err != nil {
		fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versionDir, "list"), []byte(version+"\n"), 0o644); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
