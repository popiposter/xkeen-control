package redact

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
)

const (
	maxOutboundDocument = 8 << 20
	maxSafeTagLength    = 128
	maxErrorLength      = 160
)

var safeTagPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// ReadUnifiedOutboundTags parses only the tag field of eligible outbounds.
// It intentionally does not decode or retain the rest of any outbound object.
func ReadUnifiedOutboundTags(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, maxOutboundDocument+1))
	if err != nil {
		return nil, err
	}
	if len(contents) > maxOutboundDocument {
		return nil, errors.New("outbound document exceeds bounded read size")
	}
	return UnifiedOutboundTagsJSON(contents)
}

// UnifiedOutboundTagsJSON is exported for synthetic, credential-free tests.
func UnifiedOutboundTagsJSON(contents []byte) ([]string, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(contents, &root); err != nil {
		return nil, err
	}
	var entries []json.RawMessage
	if raw, ok := root["outbounds"]; ok {
		if err := json.Unmarshal(raw, &entries); err != nil {
			return nil, errors.New("outbounds is not an array")
		}
	}
	if raw, ok := root["nodes"]; ok {
		var registryEntries []json.RawMessage
		if err := json.Unmarshal(raw, &registryEntries); err != nil {
			return nil, errors.New("nodes is not an array")
		}
		entries = append(entries, registryEntries...)
	}

	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		// The projection is deliberately a one-field DTO. Unknown and sensitive
		// fields are never decoded into an application object.
		var projection struct {
			Tag         string `json:"tag"`
			OutboundTag string `json:"outboundTag"`
		}
		if err := json.Unmarshal(entry, &projection); err != nil {
			continue
		}
		tag := projection.Tag
		if tag == "" {
			tag = projection.OutboundTag
		}
		if IsUnifiedOutboundTag(tag) {
			seen[tag] = struct{}{}
		}
	}

	tags := make([]string, 0, len(seen))
	for tag := range seen {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return tags, nil
}

func IsUnifiedOutboundTag(tag string) bool {
	if tag == "" || len(tag) > maxSafeTagLength || !safeTagPattern.MatchString(tag) {
		return false
	}
	return strings.HasPrefix(tag, "proxy-")
}

// SanitizeError turns an Xray probe reason into a bounded class. Raw probe
// strings can contain endpoints or implementation details and are never
// suitable for an API response or log line.
func SanitizeError(reason string) string {
	reason = strings.ToLower(strings.TrimSpace(reason))
	if reason == "" {
		return ""
	}
	if len(reason) > maxErrorLength {
		reason = reason[:maxErrorLength]
	}
	switch {
	case strings.Contains(reason, "timeout"), strings.Contains(reason, "deadline"):
		return "timeout"
	case strings.Contains(reason, "refused"):
		return "connection-refused"
	case strings.Contains(reason, "tls"), strings.Contains(reason, "certificate"):
		return "tls"
	case strings.Contains(reason, "dns"), strings.Contains(reason, "resolve"):
		return "dns"
	case strings.Contains(reason, "handshake"):
		return "handshake"
	case strings.Contains(reason, "network"), strings.Contains(reason, "connect"):
		return "network"
	default:
		return "probe-failed"
	}
}
