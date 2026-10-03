package configjson

import (
	"strings"
	"testing"
)

func TestReplacePathPreservesCommentsAndOpaqueSiblings(t *testing.T) {
	source := []byte("{/* outer */\n\"dns\": {\"queryStrategy\": \"UseIP\", // keep\n\"future\":{\"big\":9007199254740993}},\"other\":true}")
	changed, err := ReplacePath(source, []string{"dns", "queryStrategy"}, "UseIPv4")
	if err != nil {
		t.Fatal(err)
	}
	if string(changed) != strings.Replace(string(source), "\"UseIP\"", "\"UseIPv4\"", 1) {
		t.Fatal("modified unrelated bytes", string(changed))
	}
	inserted, err := ReplacePath(changed, []string{"dns", "disableCache"}, true)
	if err != nil || !strings.Contains(string(inserted), "// keep\n\"future\":{\"big\":9007199254740993}") {
		t.Fatal("insertion lost opaque bytes", err)
	}
	if _, err := ReplacePath([]byte(`{"dns":{}}`), []string{"dns", "disableCache"}, false); err != nil {
		t.Fatal(err)
	}
}

func TestReplacePathRejectsAmbiguousOrMissingIntermediateObjects(t *testing.T) {
	for _, source := range []string{`{"dns":{"x":1,"x":2}}`, `{"dns":null}`, `{}`} {
		if _, err := ReplacePath([]byte(source), []string{"dns", "disableCache"}, false); err == nil {
			t.Fatal("accepted ambiguous path", source)
		}
	}
}
