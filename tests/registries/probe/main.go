// This test driver accepts synthetic fixture text and returns parser observations.
package main

import (
	"encoding/json"
	"github.com/war-and-code/dircue/pkg/registries"
	"os"
)

func main() {
	var cases []struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(os.Stdin).Decode(&cases); err != nil {
		panic("invalid test input")
	}
	results := make([]registries.Configuration, 0, len(cases))
	for _, fixture := range cases {
		value, err := registries.Parse(".npmrc", []byte(fixture.Text))
		if err != nil {
			panic("test parser failed")
		}
		results = append(results, value)
	}
	if err := json.NewEncoder(os.Stdout).Encode(results); err != nil {
		panic("test output failed")
	}
}
