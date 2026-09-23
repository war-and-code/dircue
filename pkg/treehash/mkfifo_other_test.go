//go:build !unix

package treehash

import "errors"

func syscallMkfifo(string) error { return errors.New("no FIFOs on this platform") }
