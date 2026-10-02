package configjson

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNativeJSONCPreservesUnknownValuesAndStringCommentMarkers(t *testing.T) {
	input := []byte("// native template\n{\"url\":\"https://fixture.invalid/a/*b//c\",/* note */\"extra\":{\"number\":9007199254740993},\"escaped\":\"quote \\\" // literal\"}")
	got, err := DecodeObject(input)
	if err != nil {
		t.Fatal(err)
	}
	var extra map[string]json.RawMessage
	if json.Unmarshal(got["extra"], &extra) != nil || string(extra["number"]) != "9007199254740993" {
		t.Fatal("unknown number lost precision")
	}
	if !strings.Contains(string(got["url"]), "/*b//c") {
		t.Fatal("stripped string contents")
	}
}

func TestNativeJSONCRejectsAmbiguousMalformedAndDeepDocuments(t *testing.T) {
	for _, input := range []string{`null`, `[]`, `{} {}`, `{"a":1,"a":2}`, `{"a":{"b":1,"b":2}}`, `{"a":[{"b":1,"b":2}]}`, `{/* unterminated`, `{"a":1,}`, `{"a":"unterminated}`, "{\"a\":\"\xff\"}", `{"a":` + strings.Repeat("[", 66) + "0" + strings.Repeat("]", 66) + "}"} {
		if _, err := DecodeObject([]byte(input)); err == nil {
			t.Fatalf("accepted invalid configuration %q", input)
		}
	}
}
