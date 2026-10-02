package xkeen

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"
)

var errNativeConfig = errors.New("native configuration is invalid or unsupported")

// decodeNativeObject accepts the comments used in native XKeen/Xray templates.
// It preserves unknown values as RawMessage and rejects duplicate keys rather
// than silently changing their meaning during a later scoped edit.
func decodeNativeObject(input []byte) (map[string]json.RawMessage, error) {
	if !utf8.Valid(input) {
		return nil, errNativeConfig
	}
	clean, err := stripNativeComments(input)
	if err != nil {
		return nil, errNativeConfig
	}
	decoder := json.NewDecoder(bytes.NewReader(clean))
	decoder.UseNumber()
	if err := validateNativeValue(decoder, 0); err != nil {
		return nil, errNativeConfig
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errNativeConfig
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(clean, &object) != nil || object == nil {
		return nil, errNativeConfig
	}
	return object, nil
}

func validateNativeValue(d *json.Decoder, depth int) error {
	if depth > 64 {
		return errNativeConfig
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, container := token.(json.Delim)
	if !container {
		return nil
	}
	switch delim {
	case '{':
		keys := make(map[string]bool)
		for d.More() {
			token, err := d.Token()
			if err != nil {
				return err
			}
			key, ok := token.(string)
			if !ok || keys[key] {
				return errNativeConfig
			}
			keys[key] = true
			if err := validateNativeValue(d, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := validateNativeValue(d, depth+1); err != nil {
				return err
			}
		}
	default:
		return errNativeConfig
	}
	end, err := d.Token()
	if err != nil || (delim == '{' && end != json.Delim('}')) || (delim == '[' && end != json.Delim(']')) {
		return errNativeConfig
	}
	return nil
}

func stripNativeComments(input []byte) ([]byte, error) {
	output := append([]byte(nil), input...)
	inString, escaped := false, false
	for i := 0; i < len(input); i++ {
		c := input[i]
		if inString {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			continue
		}
		if c != '/' || i+1 >= len(input) {
			continue
		}
		if input[i+1] == '/' {
			for ; i < len(input) && input[i] != '\n' && input[i] != '\r'; i++ {
				output[i] = ' '
			}
			i--
		} else if input[i+1] == '*' {
			output[i], output[i+1] = ' ', ' '
			i += 2
			for ; i+1 < len(input) && !(input[i] == '*' && input[i+1] == '/'); i++ {
				if input[i] != '\n' && input[i] != '\r' {
					output[i] = ' '
				}
			}
			if i+1 >= len(input) {
				return nil, errNativeConfig
			}
			output[i], output[i+1] = ' ', ' '
			i++
		}
	}
	return output, nil
}
