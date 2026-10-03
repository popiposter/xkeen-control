package configjson

import (
	"bytes"
	"encoding/json"
)

// ReplacePath edits a known object's data field while retaining all bytes outside
// that field, including comments and opaque sibling values. Paths are supplied
// by typed editors, never by HTTP clients.
func ReplacePath(input []byte, path []string, value any) ([]byte, error) {
	if len(path) == 0 {
		return nil, errNativeConfig
	}
	if _, err := DecodeObject(input); err != nil {
		return nil, err
	}
	clean, err := stripNativeComments(input)
	if err != nil {
		return nil, err
	}
	start, end, found, err := fieldRange(clean, path)
	if err != nil {
		return nil, err
	}
	replacement, err := json.Marshal(value)
	if err != nil {
		return nil, errNativeConfig
	}
	if !found {
		// Insert only the leaf; a missing intermediate object remains unsupported.
		key, _ := json.Marshal(path[len(path)-1])
		prefix := []byte("\n")
		if start != end {
			prefix = []byte(",\n")
		}
		replacement = append(append(append(prefix, key...), ':'), replacement...)
		start = end
	}
	out := append([]byte(nil), input[:start]...)
	out = append(out, replacement...)
	out = append(out, input[end:]...)
	if _, err := DecodeObject(out); err != nil {
		return nil, err
	}
	return out, nil
}

func fieldRange(clean []byte, path []string) (int, int, bool, error) {
	d := json.NewDecoder(bytes.NewReader(clean))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return 0, 0, false, errNativeConfig
	}
	nonempty := false
	for d.More() {
		nonempty = true
		key, err := d.Token()
		if err != nil {
			return 0, 0, false, errNativeConfig
		}
		start := int(d.InputOffset())
		for start < len(clean) && (clean[start] == ':' || clean[start] == ' ' || clean[start] == '\n' || clean[start] == '\r' || clean[start] == '\t') {
			start++
		}
		var raw json.RawMessage
		if d.Decode(&raw) != nil {
			return 0, 0, false, errNativeConfig
		}
		end := int(d.InputOffset())
		if key != path[0] {
			continue
		}
		if len(path) == 1 {
			return start, end, true, nil
		}
		a, b, found, err := fieldRange(clean[start:end], path[1:])
		return start + a, start + b, found, err
	}
	if len(path) != 1 {
		return 0, 0, false, errNativeConfig
	}
	position := int(d.InputOffset())
	for position < len(clean) && clean[position] != '}' {
		position++
	}
	if position >= len(clean) {
		return 0, 0, false, errNativeConfig
	}
	start := position
	if nonempty {
		start--
	} // Encodes whether the insertion needs a separator.
	return start, position, false, nil
}
