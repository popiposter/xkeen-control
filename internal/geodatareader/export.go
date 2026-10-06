package geodatareader

import (
	"bufio"
	"context"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"google.golang.org/protobuf/encoding/protowire"
)

// ExportDomains reads selected categories in one bounded pass. It shares Query's
// input/parser and drift checks, without pagination or a persistent index.
func (r *Reader) ExportDomains(ctx context.Context, file string, categories []string) (map[string][]string, string, error) {
	if !filename.MatchString(file) || !strings.HasPrefix(file, "geosite") || len(categories) > 256 {
		return nil, "", ErrUnavailable
	}
	wanted := map[string]string{}
	for _, category := range categories {
		parts := strings.SplitN(category, "@", 2)
		if !codeName.MatchString(parts[0]) || len(parts) == 2 && !codeName.MatchString(strings.TrimPrefix(parts[1], "!")) {
			return nil, "", ErrUnavailable
		}
		wanted[category] = strings.ToLower(parts[0])
	}
	r.once.Do(func() { r.gate = make(chan struct{}, 1) })
	select {
	case r.gate <- struct{}{}:
		defer func() { <-r.gate }()
	case <-ctx.Done():
		return nil, "", ErrUnavailable
	}
	path := filepath.Join(r.Dir, file)
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > MaxFile {
		return nil, "", ErrUnavailable
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, "", ErrUnavailable
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return nil, "", ErrUnavailable
	}
	digest, err := hashFile(ctx, f)
	if err != nil {
		return nil, "", err
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return nil, "", ErrUnavailable
	}
	reader := bufio.NewReaderSize(f, 64<<10)
	result := map[string][]string{}
	seen := map[string]bool{}
	total := 0
	bytes := 0
	for {
		if ctx.Err() != nil {
			return nil, "", ErrUnavailable
		}
		tag, err := binary.ReadUvarint(reader)
		if err == io.EOF {
			break
		}
		if err != nil || tag != 10 {
			return nil, "", ErrUnavailable
		}
		size, err := binary.ReadUvarint(reader)
		if err != nil || size == 0 || size > maxEntry {
			return nil, "", ErrUnavailable
		}
		entry := make([]byte, int(size))
		if _, err = io.ReadFull(reader, entry); err != nil {
			return nil, "", ErrUnavailable
		}
		code, inverse, _, err := categoryHeader(entry)
		if err != nil || seen[code] || len(seen) >= 8192 {
			return nil, "", ErrUnavailable
		}
		seen[code] = true
		var selectors []string
		for selector, category := range wanted {
			if category == code {
				selectors = append(selectors, selector)
				result[selector] = []string{}
			}
		}
		if len(selectors) == 0 {
			continue
		}
		if inverse {
			return nil, "", ErrUnavailable
		}
		err = fields(entry, func(n protowire.Number, typ protowire.Type, b []byte, _ uint64) error {
			if n != 2 {
				return nil
			}
			if typ != protowire.BytesType || ctx.Err() != nil {
				return ErrUnavailable
			}
			item, err := parseItem("geosite", b)
			if err != nil || strings.ContainsAny(item.Value, "\r\n") {
				return ErrUnavailable
			}
			kind := item.Type
			if kind == "substring" {
				kind = "keyword"
			}
			for _, selector := range selectors {
				parts := strings.SplitN(selector, "@", 2)
				if len(parts) == 2 {
					attr := strings.TrimPrefix(parts[1], "!")
					found := false
					for _, a := range item.Attributes {
						if strings.EqualFold(a, attr) {
							found = true
						}
					}
					if found == strings.HasPrefix(parts[1], "!") {
						continue
					}
				}
				total++
				bytes += len(item.Value) + len(kind) + 2
				if total > 500000 || bytes > 16<<20 {
					return ErrUnavailable
				}
				result[selector] = append(result[selector], kind+":"+item.Value)
			}
			return nil
		})
		if err != nil {
			return nil, "", ErrUnavailable
		}
	}
	for selector := range wanted {
		if _, ok := result[selector]; !ok {
			return nil, "", ErrUnavailable
		}
		sort.Strings(result[selector])
	}
	end, err := hashFile(ctx, f)
	after, pathErr := os.Lstat(path)
	if err != nil || pathErr != nil || digest != end || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return nil, "", ErrUnavailable
	}
	return result, digest, nil
}
