package providerjoin

import (
	"testing"

	"dircue/pkg/mapdoc"
)

func TestBindingRequiresEverySuppliedIdentityToBeComparable(t *testing.T) {
	tests := []struct {
		name     string
		snapshot Snapshot
		identity reportIdentity
		want     Binding
	}{
		{
			name:     "matching commit does not hide unavailable tree",
			snapshot: Snapshot{Mode: "git", Commit: "same"},
			identity: reportIdentity{Commit: "same", Tree: "tree"},
			want:     BindingUnknown,
		},
		{
			name:     "mismatch wins over unavailable tree",
			snapshot: Snapshot{Mode: "git", Commit: "selected"},
			identity: reportIdentity{Commit: "other", Tree: "tree"},
			want:     BindingMismatch,
		},
		{
			name:     "incomplete selected digest is not comparable",
			snapshot: Snapshot{Mode: "directory", Digest: &mapdoc.Digest{Algorithm: "git-sha1", Scope: "all_regular_files", Value: "digest"}},
			identity: reportIdentity{Digest: "digest", Algorithm: "git-sha1", Scope: "all_regular_files"},
			want:     BindingUnknown,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, _ := binding(test.snapshot, test.identity)
			if got != test.want {
				t.Fatalf("binding = %s, want %s", got, test.want)
			}
		})
	}
}
