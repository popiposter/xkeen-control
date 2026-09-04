package components

import (
	"errors"
	"path"
	"strings"
)

// The E1 catalog is deliberately pinned to the reviewed build identity. The
// product archive SHA-256, reviewed member manifest and canonical installed
// generation digest remain empty until independently reproduced and reviewed.
// Consequently this entry is metadata-only and cannot be installed.
const (
	xkeenCatalogRepository     = "jameszeroX/XKeen"
	xkeenCatalogChannel        = "dev"
	xkeenCatalogTag            = "Beta"
	xkeenCatalogVersion        = "2.0.1"
	xkeenCatalogBuildCommit    = "e461c4e9964fb8ac78e5fe01aa2e27ab980af712"
	xkeenCatalogSourceParent   = "bb4060d6a87364eff8314fa723a168454df372bd"
	xkeenCatalogAsset          = "test/xkeen.tar.gz"
	xkeenCatalogBlobSHA        = "e6218668692c41565d288bf3a0bc6a420650edbd"
	xkeenCatalogAssetSize      = int64(111409)
	xkeenCatalogArchiveSHA256  = ""
	xkeenCatalogGenerationSHA  = ""
	xkeenCatalogLifecycleClass = "preserved-s05-v1"
	xkeenCatalogCompatibility  = "panel-xray-managed-runtime-v1"
)

// xkeenCatalogCommit is retained as a local spelling for code that deals in
// the fixed build commit, never a moving branch or release endpoint.
const xkeenCatalogCommit = xkeenCatalogBuildCommit

type xkeenCompatibilityEntry struct {
	Repository         string
	Channel            string
	Tag                string
	Version            string
	CommitSHA          string
	SourceParentSHA    string
	AssetName          string
	BlobSHA            string
	SizeBytes          int64
	SHA256             string
	GenerationSHA256   string
	Installable        bool
	ArchiveMembers     []XKeenArchiveMember
	LifecycleClass     string
	CompatibilityClass string
}

type XKeenArchiveMember struct {
	Name string
	Type string
	Mode uint32
	Size int64
}

const (
	xkeenArchiveDirectory = "directory"
	xkeenArchiveRegular   = "regular"
)

var reviewedXKeenCompatibility = map[string]xkeenCompatibilityEntry{
	xkeenCompatibilityKey(xkeenCatalogBuildCommit, xkeenCatalogAsset): {
		Repository:         xkeenCatalogRepository,
		Channel:            xkeenCatalogChannel,
		Tag:                xkeenCatalogTag,
		Version:            xkeenCatalogVersion,
		CommitSHA:          xkeenCatalogBuildCommit,
		SourceParentSHA:    xkeenCatalogSourceParent,
		AssetName:          xkeenCatalogAsset,
		BlobSHA:            xkeenCatalogBlobSHA,
		SizeBytes:          xkeenCatalogAssetSize,
		SHA256:             xkeenCatalogArchiveSHA256,
		GenerationSHA256:   xkeenCatalogGenerationSHA,
		Installable:        false,
		ArchiveMembers:     nil,
		LifecycleClass:     xkeenCatalogLifecycleClass,
		CompatibilityClass: xkeenCatalogCompatibility,
	},
}

func xkeenCompatibilityKey(buildCommit, asset string) string { return buildCommit + "\x00" + asset }

func reviewedXKeenEntry(buildCommit, asset string) (xkeenCompatibilityEntry, bool) {
	entry, ok := reviewedXKeenCompatibility[xkeenCompatibilityKey(buildCommit, asset)]
	return entry, ok
}

func validateXKeenCompatibilityEntry(entry xkeenCompatibilityEntry) error {
	if entry.Repository != xkeenCatalogRepository || entry.Channel != xkeenCatalogChannel || entry.AssetName != xkeenCatalogAsset ||
		entry.Tag == "" || len(entry.Tag) > 128 || entry.Version == "" || len(entry.Version) > 128 ||
		entry.LifecycleClass != xkeenCatalogLifecycleClass || entry.CompatibilityClass != xkeenCatalogCompatibility ||
		entry.SizeBytes <= 0 || entry.SizeBytes > MaxXKeenArchiveBytes {
		return errors.New("invalid XKeen compatibility catalog identity")
	}
	if entry.Installable {
		if !isHexSHA256(entry.SHA256) || !isHexSHA256(entry.GenerationSHA256) {
			return errors.New("installable XKeen entry lacks pinned digests")
		}
		if len(entry.ArchiveMembers) == 0 || len(entry.ArchiveMembers) > MaxXKeenArchiveEntries {
			return errors.New("installable XKeen entry lacks reviewed archive layout")
		}
	} else if entry.SHA256 != "" || entry.GenerationSHA256 != "" || len(entry.ArchiveMembers) != 0 {
		return errors.New("non-installable XKeen entry contains partial qualification")
	}
	if !isHexSHA1(entry.CommitSHA) || !isHexSHA1(entry.SourceParentSHA) || !isHexSHA1(entry.BlobSHA) {
		return errors.New("invalid XKeen catalog Git identity")
	}
	if entry.Installable {
		seen := make(map[string]struct{}, len(entry.ArchiveMembers))
		hasBinary := false
		hasModuleRoot := false
		for _, member := range entry.ArchiveMembers {
			if member.Name == "" || member.Size < 0 || member.Size > MaxXKeenArchiveMemberBytes ||
				!validXKeenArchiveMemberMode(member) || (member.Type != xkeenArchiveDirectory && member.Type != xkeenArchiveRegular) {
				return errors.New("invalid XKeen compatibility catalog member")
			}
			if member.Type == xkeenArchiveDirectory && member.Size != 0 {
				return errors.New("XKeen directory catalog member has a non-zero size")
			}
			if _, ok := seen[member.Name]; ok {
				return errors.New("duplicate XKeen compatibility catalog member")
			}
			seen[member.Name] = struct{}{}
			if err := validateXKeenArchiveName(member.Name, member.Type); err != nil {
				return err
			}
			if member.Name == "xkeen" && member.Type == xkeenArchiveRegular {
				hasBinary = true
			}
			if member.Name == "_xkeen/" && member.Type == xkeenArchiveDirectory {
				hasModuleRoot = true
			}
		}
		if !hasBinary || !hasModuleRoot {
			return errors.New("installable XKeen entry lacks required pair roots")
		}
	}
	return nil
}

// The reviewed upstream packaging preserves ordinary module files as 0644,
// makes the top-level executable 0755, and creates directories as 0755. The
// catalog still pins one exact mode on every member; this helper only rejects
// modes outside that reviewed packaging vocabulary.
func validXKeenArchiveMemberMode(member XKeenArchiveMember) bool {
	switch member.Type {
	case xkeenArchiveDirectory:
		return member.Mode == 0o755
	case xkeenArchiveRegular:
		return member.Mode == 0o644 || member.Mode == 0o755
	default:
		return false
	}
}

func installableXKeenEntry(identity XKeenReleaseIdentity) (xkeenCompatibilityEntry, error) {
	entry, ok := reviewedXKeenEntry(identity.CommitSHA, identity.AssetName)
	if !ok || validateXKeenCompatibilityEntry(entry) != nil || !entry.Installable {
		return xkeenCompatibilityEntry{}, ErrXKeenArtifactRejected
	}
	return entry, nil
}

func installableXKeenEntryForGeneration(generation string) (xkeenCompatibilityEntry, bool) {
	if !isHexSHA256(generation) {
		return xkeenCompatibilityEntry{}, false
	}
	for _, entry := range reviewedXKeenCompatibility {
		if entry.Installable && strings.EqualFold(entry.GenerationSHA256, generation) && validateXKeenCompatibilityEntry(entry) == nil {
			return entry, true
		}
	}
	return xkeenCompatibilityEntry{}, false
}

func validateXKeenArchiveName(name, memberType string) error {
	if strings.ContainsRune(name, '\\') || strings.HasPrefix(name, "/") || strings.Contains(name, "//") ||
		strings.Contains(name, "../") || strings.HasPrefix(name, "../") || name == "." || name == ".." {
		return errors.New("unsafe XKeen archive member name")
	}
	if memberType == xkeenArchiveDirectory {
		if !strings.HasSuffix(name, "/") {
			return errors.New("directory member is not slash-terminated")
		}
		name = strings.TrimSuffix(name, "/")
		if name == "xkeen" {
			return errors.New("xkeen binary cannot be a directory")
		}
	} else if strings.HasSuffix(name, "/") {
		return errors.New("regular member is slash-terminated")
	}
	if memberType == xkeenArchiveRegular && name == "_xkeen" {
		return errors.New("module root cannot be a regular file")
	}
	if name != "xkeen" && name != "_xkeen" && !strings.HasPrefix(name, "_xkeen/") {
		return errors.New("XKeen archive member outside reviewed allowlist")
	}
	clean := path.Clean(name)
	if clean != name || clean == "." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return errors.New("unsafe normalized XKeen archive member name")
	}
	return nil
}

func init() {
	initial, ok := reviewedXKeenCompatibility[xkeenCompatibilityKey(xkeenCatalogBuildCommit, xkeenCatalogAsset)]
	if !ok || initial.Repository != xkeenCatalogRepository || initial.Channel != xkeenCatalogChannel || initial.Tag != xkeenCatalogTag ||
		initial.Version != xkeenCatalogVersion || initial.CommitSHA != xkeenCatalogBuildCommit || initial.SourceParentSHA != xkeenCatalogSourceParent ||
		initial.AssetName != xkeenCatalogAsset || initial.BlobSHA != xkeenCatalogBlobSHA || initial.SizeBytes != xkeenCatalogAssetSize || initial.Installable {
		panic("invalid initial XKeen catalog identity")
	}
	for key, entry := range reviewedXKeenCompatibility {
		if key != xkeenCompatibilityKey(entry.CommitSHA, entry.AssetName) || validateXKeenCompatibilityEntry(entry) != nil {
			panic("invalid fixed XKeen compatibility catalog")
		}
	}
}
