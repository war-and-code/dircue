// The classifier-window probe exposes the maintained Enry ranking on exact
// bytes. It is conformance infrastructure, not a shipped command.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	enry "github.com/war-and-code/dircue/third_party/go-enry"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: classifier-window-probe content-file")
		os.Exit(2)
	}
	content, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ranking := enry.GetLanguagesByClassifier(os.Args[1], content, []string{"Java", "Perl"})
	if err := json.NewEncoder(os.Stdout).Encode(map[string][]string{"ranking": ranking}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
