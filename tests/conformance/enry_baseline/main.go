package main

import (
	"encoding/json"
	enry "github.com/go-enry/go-enry/v2"
	"io/fs"
	"os"
	"path/filepath"
)

func main() {
	r := map[string]string{}
	err := filepath.WalkDir(os.Args[1], func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.Type().IsRegular() {
			return nil
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(os.Args[1], p)
		if e != nil {
			return e
		}
		r[filepath.ToSlash(rel)] = enry.GetLanguage(filepath.Base(p), b)
		return nil
	})
	if err != nil {
		panic(err)
	}
	json.NewEncoder(os.Stdout).Encode(r)
}
