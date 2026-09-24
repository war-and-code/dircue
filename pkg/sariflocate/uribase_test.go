package sariflocate

import "testing"

func TestResolveURIIgnoresBaseOnAbsoluteURI(t *testing.T) {
	bases := baseURIs(rawObject{}, nil)
	rel, status := resolveURI("/src/app/main.py", "%SRCROOT%", bases, "/src", true)
	if status != "" || rel != "app/main.py" {
		t.Fatalf("resolveURI = %q, %q; want app/main.py", rel, status)
	}
}

func TestResolveURISuppliedBaseDefinesUndeclaredID(t *testing.T) {
	if _, status := resolveURI("app/main.py", "%SRCROOT%", baseURIs(rawObject{}, nil), "", true); status != "unresolvable_uri" {
		t.Fatalf("undeclared base without a supplied value = %q, want unresolvable_uri", status)
	}
	bases := baseURIs(rawObject{}, map[string]string{"SRCROOT": "."})
	rel, status := resolveURI("app/main.py", "%SRCROOT%", bases, "", true)
	if status != "" || rel != "app/main.py" {
		t.Fatalf("resolveURI = %q, %q; want app/main.py", rel, status)
	}
}

func TestResolveURIDeclaredBaseWinsOverSupplied(t *testing.T) {
	run := rawObject{"originalUriBaseIds": []byte(`{"%SRCROOT%":{"uri":"sub/"}}`)}
	bases := baseURIs(run, map[string]string{"SRCROOT": "."})
	rel, status := resolveURI("main.py", "%SRCROOT%", bases, "", true)
	if status != "" || rel != "sub/main.py" {
		t.Fatalf("resolveURI = %q, %q; want sub/main.py", rel, status)
	}
}
