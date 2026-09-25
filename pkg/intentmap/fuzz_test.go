package intentmap

import (
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

func FuzzCandidateParsersDeterministic(f *testing.F) {
	f.Add(byte(0), []byte("syntax = \"proto3\"; package sample.v1; service API { rpc Get(Request) returns (Reply); }"))
	f.Add(byte(1), []byte("package sample\nimport (\n  \"net/http\"\n  \"github.com/redis/go-redis/v9\"\n)\n"))
	f.Add(byte(2), []byte("DATABASE_URL=postgres://ignored\nPORT=8080\n"))
	f.Add(byte(3), []byte(`{"ConnectionStrings":{"Primary":"ignored"},"Server":{"Port":8080}}`))
	f.Add(byte(0), []byte{0, 0xff, '/', '*', '\n'})
	f.Fuzz(func(t *testing.T, kind byte, content []byte) {
		if len(content) > DefaultMaxFileBytes {
			return
		}
		first, firstLimited := fuzzParseCandidate(kind, content)
		second, secondLimited := fuzzParseCandidate(kind, content)
		if firstLimited != secondLimited || !reflect.DeepEqual(first, second) {
			t.Fatalf("candidate parser is nondeterministic: limited=%t/%t", firstLimited, secondLimited)
		}
		lines := 1 + strings.Count(string(content), "\n")
		for _, observation := range first {
			if observation.Name == "" || observation.Path == "" || observation.Kind == "" {
				t.Fatalf("incomplete observation: %+v", observation)
			}
			if observation.StartLine < 0 || observation.EndLine < observation.StartLine || observation.EndLine > lines {
				t.Fatalf("observation lines outside 0..%d: %+v", lines, observation)
			}
		}
	})
}

func FuzzConfigValuesDoNotAffectObservations(f *testing.F) {
	f.Add(byte(0), []byte("first"), []byte("second"))
	f.Add(byte(4), []byte{0, '\n', '='}, []byte{0xff, ':', '#'})
	f.Fuzz(func(t *testing.T, capability byte, firstValue, secondValue []byte) {
		if len(firstValue)+len(secondValue) > 64<<10 {
			return
		}
		keys := []string{"DATABASE_URL", "REDIS_URL", "KAFKA_BROKERS", "OIDC_ISSUER", "JWT_AUDIENCE"}
		key := keys[int(capability)%len(keys)]
		first := parseConfig(".env", []byte(key+"="+hex.EncodeToString(firstValue)))
		second := parseConfig(".env", []byte(key+"="+hex.EncodeToString(secondValue)))
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("configuration value changed structural observations: %+v != %+v", first, second)
		}
	})
}

func FuzzProtoCommentsDoNotDeclareInterfaces(f *testing.F) {
	f.Add([]byte("service Fake { rpc Nope(X) returns (Y); }"))
	f.Add([]byte{0, 0xff, '\n', '/', '*'})
	f.Fuzz(func(t *testing.T, comment []byte) {
		if len(comment) > 64<<10 {
			return
		}
		declaration := "service Real { rpc Call(Request) returns (Reply); }"
		want := parseProto("api.proto", []byte(declaration))
		withComment := "/*" + hex.EncodeToString(comment) + "*/" + declaration
		got := parseProto("api.proto", []byte(withComment))
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("comment content changed declarations: %+v != %+v", got, want)
		}
	})
}

func fuzzParseCandidate(kind byte, content []byte) ([]Observation, bool) {
	switch kind % 4 {
	case 0:
		return parseProto("api.proto", content), false
	case 1:
		return parseGoImports("main.go", content), false
	case 2:
		return parseConfig(".env", content), false
	default:
		return parseJSONConfigBounded("appsettings.json", content)
	}
}
