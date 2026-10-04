package nativebackup

import (
	"encoding/json"
	"sort"
	"strconv"

	"github.com/popiposter/xkeen-control/internal/backup"
	"github.com/popiposter/xkeen-control/internal/configjson"
)

func interfaceReferences(files map[string][]byte) ([]string, error) {
	seen := map[string]bool{}
	for _, data := range files {
		object, err := configjson.DecodeObject(data)
		if err != nil {
			return nil, backup.ErrInvalidBundle
		}
		for _, section := range []string{"inbounds", "outbounds"} {
			if raw, ok := object[section]; ok {
				var entries []struct {
					StreamSettings struct {
						Sockopt struct {
							Interface string `json:"interface"`
						} `json:"sockopt"`
					} `json:"streamSettings"`
				}
				if json.Unmarshal(raw, &entries) != nil {
					return nil, backup.ErrInvalidBundle
				}
				for _, entry := range entries {
					name := entry.StreamSettings.Sockopt.Interface
					if len(name) > 64 {
						return nil, backup.ErrInvalidBundle
					}
					if name != "" {
						seen[name] = true
					}
				}
			}
		}
	}
	if len(seen) > 64 {
		return nil, backup.ErrInvalidBundle
	}
	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}
	sort.Strings(result)
	return result, nil
}

// Only native socket interface fields are mapped, never arbitrary JSON paths.
// A referenced interface must be an actual destination interface after mapping.
func mapInterfaces(files map[string][]byte, mapping map[string]string, available []string) error {
	if len(mapping) > 64 || len(available) > 256 {
		return backup.ErrInvalidBundle
	}
	known, used := map[string]bool{}, map[string]bool{}
	for _, name := range available {
		known[name] = true
	}
	for name, data := range files {
		object, err := configjson.DecodeObject(data)
		if err != nil {
			return backup.ErrInvalidBundle
		}
		for _, section := range []string{"inbounds", "outbounds"} {
			raw, ok := object[section]
			if !ok {
				continue
			}
			var entries []json.RawMessage
			if json.Unmarshal(raw, &entries) != nil {
				return backup.ErrInvalidBundle
			}
			for index, entry := range entries {
				var value struct {
					StreamSettings struct {
						Sockopt struct {
							Interface string `json:"interface"`
						} `json:"sockopt"`
					} `json:"streamSettings"`
				}
				if json.Unmarshal(entry, &value) != nil {
					return backup.ErrInvalidBundle
				}
				from := value.StreamSettings.Sockopt.Interface
				if from == "" {
					continue
				}
				if len(from) > 64 {
					return backup.ErrInvalidBundle
				}
				to := from
				if replacement, exists := mapping[from]; exists {
					to = replacement
					used[from] = true
				}
				if !known[to] {
					return backup.ErrInvalidBundle
				}
				if to != from {
					replaced, err := configjson.ReplacePath(data, []string{section, strconv.Itoa(index), "streamSettings", "sockopt", "interface"}, to)
					if err != nil {
						return backup.ErrInvalidBundle
					}
					data = replaced
				}
			}
		}
		files[name] = data
	}
	for from, to := range mapping {
		if !used[from] || len(to) > 64 {
			return backup.ErrInvalidBundle
		}
	}
	return nil
}
