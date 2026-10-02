package xkeen

import (
	"encoding/json"
	"github.com/popiposter/xkeen-control/internal/configjson"
)

func decodeNativeObject(input []byte) (map[string]json.RawMessage, error) {
	return configjson.DecodeObject(input)
}
