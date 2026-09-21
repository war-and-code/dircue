package cli

import "testing"

func TestTerminalValuesPreserveOrdinaryPaths(t *testing.T) {
	for _, value := range []string{"src/main.go", "hello world.cs", "café.py", "a:b.go"} {
		if terminalValue(value) != value {
			t.Fatal(value)
		}
	}
	for _, value := range []string{"x\nforged", "\x1b[2J", "a\u202eb", "x\x00z", "x\xffz"} {
		result := terminalValue(value)
		if result == value {
			t.Fatalf("unescaped %q", value)
		}
		for _, r := range result {
			if r < 32 || r == 127 || r == '\u202e' {
				t.Fatalf("unsafe %q", result)
			}
		}
	}
}

func TestSourceControlFlagValidation(t *testing.T) {
	for _, flags := range [][]string{{"--tree="}, {"--on-error=maybe"}, {"--tree=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "--rev=HEAD"}, {"--tree=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "--source=directory"}} {
		_, _, err := invoke(append(flags, t.TempDir())...)
		if err == nil {
			t.Fatalf("accepted invalid source flags %v", flags)
		}
	}
}
