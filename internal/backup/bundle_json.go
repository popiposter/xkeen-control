package backup

import (
	"encoding/json"

	"github.com/popiposter/xkeen-control/internal/appliance"
	"github.com/popiposter/xkeen-control/internal/nodes"
)

// UnmarshalJSON keeps the portable bundle parser strict all the way through
// the nested secret registry. Registry.Store.Load intentionally remains more
// permissive for production schema-v1 compatibility; backup/import parsing is
// a separate fail-closed wire boundary.
func (b *Bundle) UnmarshalJSON(data []byte) error {
	if b == nil {
		return ErrInvalidBundle
	}
	var wire struct {
		Format        string              `json:"format"`
		FormatVersion int                 `json:"formatVersion"`
		Manifest      Manifest            `json:"manifest"`
		Appliance     appliance.Appliance `json:"appliance"`
		Nodes         json.RawMessage     `json:"nodes,omitempty"`
	}
	if err := decodeStrict(data, &wire); err != nil {
		return err
	}

	var registry *nodes.Registry
	if len(wire.Nodes) != 0 {
		parsed, err := nodes.ParseCanonical(wire.Nodes)
		if err != nil {
			return ErrInvalidBundle
		}
		registry = &parsed
	}
	*b = Bundle{
		Format:        wire.Format,
		FormatVersion: wire.FormatVersion,
		Manifest:      wire.Manifest,
		Appliance:     wire.Appliance,
		Nodes:         registry,
	}
	return nil
}
