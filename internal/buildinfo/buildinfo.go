package buildinfo

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

// These variables are replaced by the release build with -X. Development
// builds intentionally retain an explicit non-release identity.
var (
	Version = "dev"
	Commit  = "dev"
	Channel = "development"
)

var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

type Info struct {
	Product      string `json:"product"`
	Version      string `json:"version"`
	SourceCommit string `json:"sourceCommit"`
	Channel      string `json:"channel"`
}

func Current() Info {
	return Info{Product: "xkeen-control", Version: strings.TrimSpace(Version), SourceCommit: strings.TrimSpace(Commit), Channel: strings.TrimSpace(Channel)}
}

func (i Info) Validate() error {
	if i.Product != "xkeen-control" {
		return errors.New("wrong product")
	}
	if !validVersion(i.Version) {
		return errors.New("invalid semantic version")
	}
	switch i.Channel {
	case "development":
		if i.SourceCommit != "dev" && !commitPattern.MatchString(i.SourceCommit) {
			return errors.New("development build has invalid source commit")
		}
	case "stable":
		if !commitPattern.MatchString(i.SourceCommit) || strings.Contains(i.Version, "-") {
			return errors.New("stable build provenance is invalid")
		}
	case "beta":
		if !commitPattern.MatchString(i.SourceCommit) || !strings.Contains(i.Version, "-") {
			return errors.New("beta build provenance is invalid")
		}
	default:
		return errors.New("unsupported build channel")
	}
	return nil
}

func (i Info) JSON() ([]byte, error) {
	if err := i.Validate(); err != nil && i.Channel != "development" {
		return nil, err
	}
	contents, err := json.Marshal(i)
	if err != nil {
		return nil, err
	}
	return append(contents, '\n'), nil
}

func validVersion(value string) bool {
	value = strings.TrimPrefix(value, "v")
	if value == "" || strings.ContainsAny(value, " /\\") {
		return false
	}
	parts := strings.Split(value, "+")
	if len(parts) > 2 || parts[0] == "" || (len(parts) == 2 && !validIdentifiers(parts[1], false)) {
		return false
	}
	core := parts[0]
	if separator := strings.IndexByte(core, '-'); separator >= 0 {
		if !validIdentifiers(core[separator+1:], true) {
			return false
		}
		core = core[:separator]
	}
	numbers := strings.Split(core, ".")
	if len(numbers) != 3 {
		return false
	}
	for _, number := range numbers {
		if number == "" || (len(number) > 1 && number[0] == '0') {
			return false
		}
		for _, character := range number {
			if character < '0' || character > '9' {
				return false
			}
		}
	}
	return true
}

func validIdentifiers(value string, prerelease bool) bool {
	if value == "" {
		return false
	}
	for _, identifier := range strings.Split(value, ".") {
		if identifier == "" || (prerelease && len(identifier) > 1 && identifier[0] == '0' && allDigits(identifier)) {
			return false
		}
		for _, character := range identifier {
			if !((character >= '0' && character <= '9') || (character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z') || character == '-') {
				return false
			}
		}
	}
	return true
}

func allDigits(value string) bool {
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
