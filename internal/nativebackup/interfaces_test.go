package nativebackup

import (
	"strings"
	"testing"
)

func TestNativeInterfaceMappingChangesOnlySelectedJSONCScalars(t *testing.T) {
	input := `{
  "outbounds": [
    // First interface
    {"tag":"first", "streamSettings":{"sockopt":{"interface": "source0", /* opaque */ "extra":9007199254740993}}},
    /* unrelated entry stays byte-exact */
    {"tag":"other", "settings":{/* operator note */ "value":"keep"}},
    // Second interface, a different replacement width changes later offsets
    {"tag":"last", "streamSettings":{"sockopt":{"interface":"source0"}}}
  ],
  "extension": true // untouched
}`
	files := map[string][]byte{"04_outbounds.json": []byte(input)}
	if err := mapInterfaces(files, map[string]string{"source0": "destination-long"}, []string{"destination-long"}); err != nil {
		t.Fatal(err)
	}
	want := strings.ReplaceAll(input, `"source0"`, `"destination-long"`)
	if string(files["04_outbounds.json"]) != want {
		t.Fatal("interface mapping changed bytes outside selected scalars")
	}
}
