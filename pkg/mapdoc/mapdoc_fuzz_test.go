package mapdoc_test

import (
	"bytes"
	"strings"
	"testing"

	"dircue/pkg/mapdoc"
)

func FuzzStrictMapCanonicalRoundTrip(f *testing.F) {
	seed, err := mapdoc.Marshal(document())
	if err != nil {
		f.Fatal(err)
	}
	f.Add(seed)
	f.Add([]byte(`{"schema_version":"1.0.0","kind":"map"}`))
	f.Add([]byte(`null`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		doc, err := mapdoc.UnmarshalStrict(data)
		if err != nil {
			return
		}
		if err := mapdoc.Validate(doc); err != nil {
			t.Fatalf("accepted document does not validate: %v", err)
		}
		first, err := mapdoc.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		roundTripped, err := mapdoc.UnmarshalStrict(first)
		if err != nil {
			t.Fatalf("canonical document cannot be read: %v\n%s", err, first)
		}
		second, err := mapdoc.Marshal(roundTripped)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("canonical round trip changed output:\n%s\n%s", first, second)
		}
	})
}

func FuzzNodeIDPathEquivalence(f *testing.F) {
	f.Add("services/api/go.mod", "services/api", "go")
	f.Add("a/b", "c/d", "")
	f.Fuzz(func(t *testing.T, first, second, discriminator string) {
		if len(first)+len(second)+len(discriminator) > 4096 {
			return
		}
		paths := []string{first, second}
		want := mapdoc.NodeID(mapdoc.NodeComponent, paths, discriminator)
		permuted := mapdoc.NodeID(mapdoc.NodeComponent, []string{second, first}, discriminator)
		duplicated := mapdoc.NodeID(mapdoc.NodeComponent, []string{first, second, first}, discriminator)
		nativeSeparators := mapdoc.NodeID(mapdoc.NodeComponent, []string{
			strings.ReplaceAll(first, "/", "\\"),
			strings.ReplaceAll(second, "/", "\\"),
		}, discriminator)
		if want != permuted || want != duplicated || want != nativeSeparators {
			t.Fatalf("equivalent identity paths changed ID: %q %q %q %q", want, permuted, duplicated, nativeSeparators)
		}
	})
}
