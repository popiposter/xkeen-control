package release

import "testing"

func TestValidatePublicationVersion(t *testing.T) {
	for _, test := range []struct {
		version string
		channel string
		valid   bool
	}{
		{"0.4.25", "stable", true},
		{"0.4.25-beta.1", "beta", true},
		{"0.4.25+build-linux", "stable", true},
		{"v0.4.25", "stable", false},
		{"0garbage", "stable", false},
		{"0.4.25-", "stable", false},
		{"0.4.25-beta.1", "stable", false},
		{"0.4.25", "beta", false},
		{"0.4.25", "other", false},
	} {
		if err := ValidatePublicationVersion(test.version, test.channel); (err == nil) != test.valid {
			t.Errorf("version %q, channel %q: %v (expected valid=%t)", test.version, test.channel, err, test.valid)
		}
	}
}
