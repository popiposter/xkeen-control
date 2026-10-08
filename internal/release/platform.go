package release

import "runtime"

// Names are a closed release contract. ARM64 keeps its original schema1 URLs
// so already installed clients can continue checking signed releases.
func BinaryArtifact(architecture string) string {
	switch architecture {
	case "arm64":
		return "xkeen-control-linux-arm64"
	case "mipsle":
		return "xkeen-control-linux-mipsle"
	default:
		return ""
	}
}

func ManifestNames(architecture string) (string, string) {
	switch architecture {
	case "arm64":
		return "release-manifest.json", "release-manifest.sig"
	case "mipsle":
		return "release-manifest-mipsle.json", "release-manifest-mipsle.sig"
	default:
		return "", ""
	}
}

func ArtifactsForArchitecture(architecture string) []string {
	binary := BinaryArtifact(architecture)
	if binary == "" {
		return nil
	}
	return []string{"S99xkeen-control", "install.sh", binary, "xkeen-control-updater"}
}

func runtimeArchitecture() string {
	if runtime.GOOS == SupportedOS && BinaryArtifact(runtime.GOARCH) != "" {
		return runtime.GOARCH
	}
	return ""
}

func (c *Client) Architecture() string {
	if c == nil {
		return ""
	}
	return c.architecture
}
