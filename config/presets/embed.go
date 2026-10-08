// Package presets exposes the public policy reference used by fresh setup.
// It never contains private registry, outbounds or destination identities.
package presets

import _ "embed"

//go:embed ru-selective-v1.json
var RUSelective []byte
