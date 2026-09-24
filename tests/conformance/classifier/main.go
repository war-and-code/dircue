// The classifier probe compares the pinned Enry entry point and Dircue's
// wrapper on identical bytes. It is test infrastructure, not a shipped command.
package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/war-and-code/dircue/pkg/scanner"
	enry "github.com/war-and-code/dircue/third_party/go-enry"
)

// JSON keys retain the original evidence format across the project rename.
type result struct {
	Enry   string `json:"enry"`
	Dircue string `json:"auragaze"`
	Bytes  int    `json:"bytes"`
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: classifier-probe samples-directory")
		os.Exit(2)
	}
	root := os.Args[1]
	results := map[string]result{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unexpected nonregular sample: %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		candidate, _ := scanner.DetectLanguage(filepath.Base(path), data)
		results[filepath.ToSlash(relative)] = result{enry.GetLanguage(filepath.Base(path), data), candidate, len(data)}
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(results); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
