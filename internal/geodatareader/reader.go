// Package geodatareader reads installed Xray GeoSite/GeoIP protobuf files.
// It neither downloads databases nor writes configuration or a persistent index.
package geodatareader

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"google.golang.org/protobuf/encoding/protowire"
)

const MaxFile = 64 << 20
const maxEntry = 16 << 20

var ErrUnavailable = errors.New("installed geodata unavailable or changed")
var filename = regexp.MustCompile(`^(geosite|geoip)[A-Za-z0-9_.-]*\.dat$`)
var codeName = regexp.MustCompile(`^[A-Za-z0-9_.!-]{1,128}$`)

type Reader struct {
	Dir  string
	once sync.Once
	gate chan struct{}
}
type File struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Size      int64  `json:"size"`
	Available bool   `json:"available"`
}
type Request struct {
	File     string `json:"file"`
	View     string `json:"view"`
	Category string `json:"category"`
	Search   string `json:"search"`
	Offset   int    `json:"offset"`
	Limit    int    `json:"limit"`
	Snapshot string `json:"snapshot"`
}
type Item struct {
	prefix     netip.Prefix
	Category   string   `json:"category"`
	Type       string   `json:"type"`
	Value      string   `json:"value"`
	Count      int      `json:"count,omitempty"`
	Attributes []string `json:"attributes,omitempty"`
	Inverse    bool     `json:"inverse,omitempty"`
}
type Result struct {
	File     string `json:"file"`
	Kind     string `json:"kind"`
	Snapshot string `json:"snapshot"`
	Items    []Item `json:"items"`
	Total    int    `json:"total"`
	Offset   int    `json:"offset"`
	More     bool   `json:"more"`
}

func (r *Reader) Inventory() ([]File, error) {
	d, err := os.Open(r.Dir)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer d.Close()
	entries, err := d.ReadDir(129)
	if err != nil && err != io.EOF || len(entries) > 128 {
		return nil, ErrUnavailable
	}
	files := []File{}
	for _, e := range entries {
		match := filename.FindStringSubmatch(e.Name())
		if match == nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, ErrUnavailable
		}
		files = append(files, File{e.Name(), match[1], info.Size(), info.Mode().IsRegular() && info.Size() > 0 && info.Size() <= MaxFile})
		if len(files) > 32 {
			return nil, ErrUnavailable
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return files, nil
}

func hashFile(ctx context.Context, f *os.File) (string, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", ErrUnavailable
	}
	h := sha256.New()
	buf := make([]byte, 64<<10)
	total := int64(0)
	for {
		if ctx.Err() != nil {
			return "", ErrUnavailable
		}
		n, err := f.Read(buf)
		total += int64(n)
		if total > MaxFile {
			return "", ErrUnavailable
		}
		_, _ = h.Write(buf[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", ErrUnavailable
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Query scans one entry buffer at a time. Requests serialize to bound peak RAM;
// every response is tied to freshly hashed installed bytes, never a stale cache.
func (r *Reader) Query(ctx context.Context, q Request) (Result, error) {
	result := Result{File: q.File, Items: []Item{}, Offset: q.Offset}
	match := filename.FindStringSubmatch(q.File)
	if match == nil || q.Offset < 0 || q.Offset > 10000000 || q.Limit < 0 || q.Limit > 100 || len(q.Search) > 253 || !utf8.ValidString(q.Search) || q.Category != "" && !codeName.MatchString(q.Category) || q.View != "categories" && q.View != "entries" && q.View != "match" {
		return result, ErrUnavailable
	}
	if q.Limit == 0 {
		q.Limit = 50
	}
	result.Kind = match[1]
	var ip netip.Addr
	search := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(q.Search), "."))
	if q.View == "match" {
		if result.Kind == "geoip" {
			var err error
			ip, err = netip.ParseAddr(search)
			if err != nil {
				return result, ErrUnavailable
			}
			ip = ip.Unmap()
		} else {
			_, err := netip.ParseAddr(search)
			if search == "" || strings.ContainsAny(search, " /:@") || err == nil {
				return result, ErrUnavailable
			}
		}
	}
	r.once.Do(func() { r.gate = make(chan struct{}, 1) })
	select {
	case r.gate <- struct{}{}:
		defer func() { <-r.gate }()
	case <-ctx.Done():
		return result, ErrUnavailable
	}
	path := filepath.Join(r.Dir, q.File)
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > MaxFile {
		return result, ErrUnavailable
	}
	f, err := os.Open(path)
	if err != nil {
		return result, ErrUnavailable
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return result, ErrUnavailable
	}
	result.Snapshot, err = hashFile(ctx, f)
	if err != nil || q.Snapshot != "" && q.Snapshot != result.Snapshot {
		return result, ErrUnavailable
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return result, ErrUnavailable
	}
	reader := bufio.NewReaderSize(f, 64<<10)
	seen := map[string]bool{}
	var entry []byte
	add := func(item Item) {
		if result.Total >= q.Offset && len(result.Items) < q.Limit {
			result.Items = append(result.Items, item)
		}
		result.Total++
	}
	for {
		if ctx.Err() != nil {
			return result, ErrUnavailable
		}
		tag, err := binary.ReadUvarint(reader)
		if err == io.EOF {
			break
		}
		if err != nil || tag != 10 {
			return result, ErrUnavailable
		}
		size, err := binary.ReadUvarint(reader)
		if err != nil || size > maxEntry || size == 0 {
			return result, ErrUnavailable
		}
		if cap(entry) < int(size) {
			entry = make([]byte, int(size))
		} else {
			entry = entry[:int(size)]
		}
		if _, err = io.ReadFull(reader, entry); err != nil {
			return result, ErrUnavailable
		}
		category, inverse, count, err := categoryHeader(entry)
		if err != nil || seen[category] || len(seen) >= 8192 {
			return result, ErrUnavailable
		}
		seen[category] = true
		if q.Category != "" && !strings.EqualFold(category, q.Category) {
			continue
		}
		if q.View == "categories" {
			if search == "" || strings.Contains(category, search) {
				add(Item{Category: category, Type: "category", Value: category, Count: count, Inverse: inverse})
			}
			continue
		}
		matched := false
		err = fields(entry, func(n protowire.Number, t protowire.Type, data []byte, v uint64) error {
			if ctx.Err() != nil {
				return ErrUnavailable
			}
			if n != 2 {
				return nil
			}
			if t != protowire.BytesType {
				return ErrUnavailable
			}
			item, err := parseItem(result.Kind, data)
			if err != nil {
				return err
			}
			item.Category = category
			item.Inverse = inverse
			if item.Type == "cidr" && q.View != "match" {
				item.Value = item.prefix.String()
			}
			hit := search == "" || strings.Contains(strings.ToLower(item.Value), search)
			if q.View == "match" {
				hit, err = itemMatches(item, search, ip)
				if err != nil {
					return err
				}
			}
			if hit {
				matched = true
				if !(q.View == "match" && inverse) {
					if item.Type == "cidr" {
						item.Value = item.prefix.String()
					}
					add(item)
				}
			}
			return nil
		})
		if err != nil {
			return result, ErrUnavailable
		}
		if q.View == "match" && inverse && !matched {
			add(Item{Category: category, Type: "inverse", Value: "Outside this category's listed networks", Inverse: true})
		}
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return result, ErrUnavailable
	}
	endHash, err := hashFile(ctx, f)
	after, pathErr := os.Lstat(path)
	if err != nil || pathErr != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || endHash != result.Snapshot {
		return Result{}, ErrUnavailable
	}
	result.More = q.Offset+len(result.Items) < result.Total
	return result, nil
}

func fields(data []byte, visit func(protowire.Number, protowire.Type, []byte, uint64) error) error {
	for len(data) > 0 {
		n, t, k := protowire.ConsumeTag(data)
		if k < 0 || n <= 0 {
			return ErrUnavailable
		}
		data = data[k:]
		var bytes []byte
		var v uint64
		switch t {
		case protowire.BytesType:
			bytes, k = protowire.ConsumeBytes(data)
		case protowire.VarintType:
			v, k = protowire.ConsumeVarint(data)
		default:
			k = protowire.ConsumeFieldValue(n, t, data)
		}
		if k < 0 {
			return ErrUnavailable
		}
		if err := visit(n, t, bytes, v); err != nil {
			return err
		}
		data = data[k:]
	}
	return nil
}

func categoryHeader(data []byte) (string, bool, int, error) {
	code := ""
	inverse := false
	count := 0
	err := fields(data, func(n protowire.Number, t protowire.Type, b []byte, v uint64) error {
		switch n {
		case 1:
			if t != protowire.BytesType || code != "" || !codeName.Match(b) {
				return ErrUnavailable
			}
			code = strings.ToLower(string(b))
		case 2:
			if t != protowire.BytesType {
				return ErrUnavailable
			}
			count++
		case 3:
			if t != protowire.VarintType || v > 1 {
				return ErrUnavailable
			}
			inverse = v == 1
		}
		return nil
	})
	if code == "" {
		err = ErrUnavailable
	}
	return code, inverse, count, err
}

func parseItem(kind string, data []byte) (Item, error) {
	item := Item{}
	domainType := uint64(0)
	prefix := uint64(0)
	var ipBytes []byte
	err := fields(data, func(n protowire.Number, t protowire.Type, b []byte, v uint64) error {
		if kind == "geoip" {
			switch n {
			case 1:
				if t != protowire.BytesType || ipBytes != nil {
					return ErrUnavailable
				}
				ipBytes = b
			case 2:
				if t != protowire.VarintType {
					return ErrUnavailable
				}
				prefix = v
			}
			return nil
		}
		switch n {
		case 1:
			if t != protowire.VarintType || v > 3 {
				return ErrUnavailable
			}
			domainType = v
		case 2:
			if t != protowire.BytesType || len(b) > 2048 || !utf8.Valid(b) || item.Value != "" {
				return ErrUnavailable
			}
			item.Value = string(b)
		case 3:
			if t != protowire.BytesType || len(item.Attributes) >= 16 {
				return ErrUnavailable
			}
			key := ""
			err := fields(b, func(n protowire.Number, t protowire.Type, b []byte, v uint64) error {
				if n == 1 {
					if t != protowire.BytesType || len(b) > 64 || !utf8.Valid(b) {
						return ErrUnavailable
					}
					key = string(b)
				}
				return nil
			})
			if err != nil {
				return err
			}
			item.Attributes = append(item.Attributes, key)
		}
		return nil
	})
	if kind == "geoip" {
		ip, ok := netip.AddrFromSlice(ipBytes)
		if !ok || prefix > uint64(ip.BitLen()) {
			return item, ErrUnavailable
		}
		item.Type = "cidr"
		item.prefix = netip.PrefixFrom(ip, int(prefix)).Masked()
	} else {
		if item.Value == "" {
			return item, ErrUnavailable
		}
		item.Type = []string{"substring", "regexp", "domain", "full"}[domainType]
	}
	return item, err
}

func itemMatches(item Item, domain string, ip netip.Addr) (bool, error) {
	switch item.Type {
	case "cidr":
		return item.prefix.Contains(ip), nil
	case "substring":
		return strings.Contains(domain, strings.ToLower(item.Value)), nil
	case "full":
		return domain == strings.ToLower(item.Value), nil
	case "domain":
		value := strings.ToLower(item.Value)
		return domain == value || strings.HasSuffix(domain, "."+value), nil
	case "regexp":
		re, err := regexp.Compile(item.Value)
		if err != nil {
			return false, ErrUnavailable
		}
		return re.MatchString(domain), nil
	}
	return false, ErrUnavailable
}
