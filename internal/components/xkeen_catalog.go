package components

import (
	"errors"
	"path"
	"strings"
)

// The E2 catalog is pinned to one independently qualified immutable artifact.
// The archive digest, complete member manifest and canonical installed
// generation digest were reproduced from the exact Git blob and source
// parent before this entry was made installable.
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
	xkeenCatalogArchiveSHA256  = "efbcd977321c35191cb8d31ee5209e5911b81225352c071bad99894b3d0ccc66"
	xkeenCatalogGenerationSHA  = "341ea86523c2b4ab3c853218a90704dcbeea859bb1f7efe0195d994d0ed36c4e"
	xkeenCatalogArchiveMembers = 72
	xkeenCatalogArchiveBytes   = int64(508719)
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
		Repository:       xkeenCatalogRepository,
		Channel:          xkeenCatalogChannel,
		Tag:              xkeenCatalogTag,
		Version:          xkeenCatalogVersion,
		CommitSHA:        xkeenCatalogBuildCommit,
		SourceParentSHA:  xkeenCatalogSourceParent,
		AssetName:        xkeenCatalogAsset,
		BlobSHA:          xkeenCatalogBlobSHA,
		SizeBytes:        xkeenCatalogAssetSize,
		SHA256:           xkeenCatalogArchiveSHA256,
		GenerationSHA256: xkeenCatalogGenerationSHA,
		Installable:      true,
		ArchiveMembers: []XKeenArchiveMember{
			{Name: "_xkeen/01_info/00_info_import.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 426},
			{Name: "_xkeen/01_info/01_info_common.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 23987},
			{Name: "_xkeen/01_info/01_info_variable.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 7048},
			{Name: "_xkeen/01_info/02_info_packages.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 1556},
			{Name: "_xkeen/01_info/03_info_xkeen.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 3064},
			{Name: "_xkeen/01_info/04_info_mihomo.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 1016},
			{Name: "_xkeen/01_info/04_info_xray.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 575},
			{Name: "_xkeen/01_info/05_info_geofile.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 934},
			{Name: "_xkeen/01_info/06_info_console.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 9775},
			{Name: "_xkeen/01_info/07_info_cron.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 539},
			{Name: "_xkeen/01_info/08_info_router.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 3916},
			{Name: "_xkeen/02_install/00_install_import.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 475},
			{Name: "_xkeen/02_install/01_install_packages.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 990},
			{Name: "_xkeen/02_install/02_install_mihomo.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 6100},
			{Name: "_xkeen/02_install/02_install_xray.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 5990},
			{Name: "_xkeen/02_install/03_install_xkeen.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 2631},
			{Name: "_xkeen/02_install/04_install_geofile.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 9475},
			{Name: "_xkeen/02_install/05_install_geoipset.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 7501},
			{Name: "_xkeen/02_install/06_install_cron.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 880},
			{Name: "_xkeen/02_install/07_install_register/00_register_common.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 1560},
			{Name: "_xkeen/02_install/07_install_register/00_register_import.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 388},
			{Name: "_xkeen/02_install/07_install_register/01_register_mihomo.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 1865},
			{Name: "_xkeen/02_install/07_install_register/01_register_xray.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 979},
			{Name: "_xkeen/02_install/07_install_register/02_register_xkeen.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 6303},
			{Name: "_xkeen/02_install/07_install_register/03_register_cron.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 3230},
			{Name: "_xkeen/02_install/07_install_register/04_register_init.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 166395},
			{Name: "_xkeen/02_install/08_install_configs/00_configs_import.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 156},
			{Name: "_xkeen/02_install/08_install_configs/01_configs_install.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 1304},
			{Name: "_xkeen/02_install/08_install_configs/02_configs_xray/01_log.json", Type: xkeenArchiveRegular, Mode: 0o644, Size: 151},
			{Name: "_xkeen/02_install/08_install_configs/02_configs_xray/02_dns.json", Type: xkeenArchiveRegular, Mode: 0o644, Size: 71},
			{Name: "_xkeen/02_install/08_install_configs/02_configs_xray/03_inbounds.json", Type: xkeenArchiveRegular, Mode: 0o644, Size: 1139},
			{Name: "_xkeen/02_install/08_install_configs/02_configs_xray/04_outbounds.json", Type: xkeenArchiveRegular, Mode: 0o644, Size: 104},
			{Name: "_xkeen/02_install/08_install_configs/02_configs_xray/05_routing.json", Type: xkeenArchiveRegular, Mode: 0o644, Size: 374},
			{Name: "_xkeen/02_install/08_install_configs/02_configs_xray/06_policy.json", Type: xkeenArchiveRegular, Mode: 0o644, Size: 115},
			{Name: "_xkeen/03_delete/00_delete_import.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 303},
			{Name: "_xkeen/03_delete/01_delete_geofile.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 1308},
			{Name: "_xkeen/03_delete/02_delete_geoipset.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 1638},
			{Name: "_xkeen/03_delete/03_delete_cron.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 326},
			{Name: "_xkeen/03_delete/04_delete_configs.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 196},
			{Name: "_xkeen/03_delete/05_delete_register.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 2024},
			{Name: "_xkeen/03_delete/06_delete_tmp.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 1940},
			{Name: "_xkeen/04_tools/00_tools_import.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 593},
			{Name: "_xkeen/04_tools/01_tools_ports.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 11391},
			{Name: "_xkeen/04_tools/02_tools_delay.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 2294},
			{Name: "_xkeen/04_tools/03_tools_diagnostic.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 10995},
			{Name: "_xkeen/04_tools/05_tools_choice/00_choice_import.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 369},
			{Name: "_xkeen/04_tools/05_tools_choice/01_choice_cores.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 4965},
			{Name: "_xkeen/04_tools/05_tools_choice/02_choice_xkeen.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 18708},
			{Name: "_xkeen/04_tools/05_tools_choice/03_choice_geofile.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 9214},
			{Name: "_xkeen/04_tools/05_tools_choice/04_choice_input.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 4796},
			{Name: "_xkeen/04_tools/05_tools_choice/05_choice_cron/00_cron_import.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 218},
			{Name: "_xkeen/04_tools/05_tools_choice/05_choice_cron/01_cron_status.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 4020},
			{Name: "_xkeen/04_tools/05_tools_choice/05_choice_cron/02_cron_time.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 2765},
			{Name: "_xkeen/04_tools/06_tools_backups/00_backups_import.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 308},
			{Name: "_xkeen/04_tools/06_tools_backups/01_backups_xkeen.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 2157},
			{Name: "_xkeen/04_tools/06_tools_backups/02_backups_configs_mihomo.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 1443},
			{Name: "_xkeen/04_tools/06_tools_backups/02_backups_configs_xray.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 1419},
			{Name: "_xkeen/04_tools/07_tools_downloaders/00_downloaders_import.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 440},
			{Name: "_xkeen/04_tools/07_tools_downloaders/00_fetch_with_mirrors.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 25740},
			{Name: "_xkeen/04_tools/07_tools_downloaders/01_downloaders_mihomo.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 6566},
			{Name: "_xkeen/04_tools/07_tools_downloaders/01_downloaders_xray.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 4531},
			{Name: "_xkeen/04_tools/07_tools_downloaders/02_downloaders_xkeen.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 2183},
			{Name: "_xkeen/04_tools/08_tools_balancer/00_balancer_import.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 196},
			{Name: "_xkeen/04_tools/08_tools_balancer/01_balancer_core.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 9703},
			{Name: "_xkeen/04_tools/08_tools_balancer/02_balancer_control.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 18769},
			{Name: "_xkeen/05_tests/00_tests_import.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 201},
			{Name: "_xkeen/05_tests/01_tests_system.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 6817},
			{Name: "_xkeen/05_tests/02_tests_xports.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 2943},
			{Name: "_xkeen/05_tests/03_tests_storage.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 4000},
			{Name: "_xkeen/about.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 13321},
			{Name: "_xkeen/import.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: 1076},
			{Name: "xkeen", Type: xkeenArchiveRegular, Mode: 0o755, Size: 57831},
		},
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
		hasModuleMember := false
		for _, member := range entry.ArchiveMembers {
			if member.Name == "" || member.Type != xkeenArchiveRegular || member.Size < 0 || member.Size > MaxXKeenArchiveMemberBytes ||
				!validXKeenArchiveMemberMode(member) {
				return errors.New("invalid XKeen compatibility catalog member")
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
			if strings.HasPrefix(member.Name, "_xkeen/") && member.Type == xkeenArchiveRegular {
				hasModuleMember = true
			}
		}
		if !hasBinary || !hasModuleMember {
			return errors.New("installable XKeen entry lacks required pair roots")
		}
	}
	return nil
}

func validateFixedXKeenCatalogEntry(entry xkeenCompatibilityEntry) error {
	if entry.Repository != xkeenCatalogRepository || entry.Channel != xkeenCatalogChannel || entry.Tag != xkeenCatalogTag ||
		entry.Version != xkeenCatalogVersion || entry.CommitSHA != xkeenCatalogBuildCommit || entry.SourceParentSHA != xkeenCatalogSourceParent ||
		entry.AssetName != xkeenCatalogAsset || entry.BlobSHA != xkeenCatalogBlobSHA || entry.SizeBytes != xkeenCatalogAssetSize ||
		entry.SHA256 != xkeenCatalogArchiveSHA256 || entry.GenerationSHA256 != xkeenCatalogGenerationSHA || !entry.Installable ||
		len(entry.ArchiveMembers) != xkeenCatalogArchiveMembers {
		return errors.New("invalid fully qualified XKeen catalog entry")
	}
	if err := validateXKeenCompatibilityEntry(entry); err != nil {
		return err
	}
	bytes, err := xkeenCatalogGenerationBytes(entry)
	if err != nil || bytes != xkeenCatalogArchiveBytes {
		return errors.New("invalid qualified XKeen archive size")
	}
	return nil
}

// The pinned upstream workflow emits a GNU-tar file-only archive. It preserves
// ordinary module files as 0644 and makes only the top-level executable 0755.
// The catalog still pins one exact mode on every member; this helper only
// rejects modes outside that reviewed packaging vocabulary.
func validXKeenArchiveMemberMode(member XKeenArchiveMember) bool {
	return member.Type == xkeenArchiveRegular && (member.Mode == 0o644 || member.Mode == 0o755)
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
	if !ok || validateFixedXKeenCatalogEntry(initial) != nil {
		panic("invalid fully qualified XKeen catalog identity")
	}
	installableCount := 0
	for key, entry := range reviewedXKeenCompatibility {
		if key != xkeenCompatibilityKey(entry.CommitSHA, entry.AssetName) || validateXKeenCompatibilityEntry(entry) != nil {
			panic("invalid fixed XKeen compatibility catalog")
		}
		if entry.Installable {
			installableCount++
		}
	}
	if installableCount != 1 {
		panic("invalid installable XKeen catalog count")
	}
}
