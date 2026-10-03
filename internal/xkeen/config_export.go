package xkeen

import "context"

// ExportFilesUnderLease returns a private, lossless native snapshot. The caller
// holds the shared panel lease across its registry read as well. External CLI
// writers are detected by a final snapshot, not claimed to be excluded.
func (e *ConfigEditor) ExportFilesUnderLease(ctx context.Context) (map[string][]byte, string, error) {
	if pending, err := e.HasSavedChanges(); err != nil || pending {
		return nil, "", ErrConfig
	}
	snapshot, err := e.Snapshot(ctx)
	if err != nil {
		return nil, "", err
	}
	files := make(map[string][]byte, len(snapshot.files))
	for name, data := range snapshot.files {
		if name == registryConfigID {
			continue
		}
		if !editableConfig(name) && name != "04_outbounds.json" {
			return nil, "", ErrConfig
		}
		files[name] = append([]byte(nil), data...)
	}
	return files, snapshot.Digest, nil
}
