//go:build !minigo

// DiskBackend stores compiler cache actions and objects in a native directory.
package cache

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	actionDirectory = "actions"
	objectDirectory = "objects"
	mtimeInterval   = time.Hour
	trimInterval    = 24 * time.Hour
	trimLimit       = 5 * 24 * time.Hour
)

type DiskBackend struct {
	root string
	now  func() time.Time
}

type DiskStats struct {
	Actions int   `json:"actions"`
	Objects int   `json:"objects"`
	Bytes   int64 `json:"bytes"`
}

type diskRecord struct {
	name    string
	key     string
	size    int64
	modTime time.Time
}

func NewDiskBackend(root string) *DiskBackend {
	return &DiskBackend{root: strings.TrimSpace(root), now: time.Now}
}

func ResolveDiskRoot(explicit string) (string, error) {
	if root := strings.TrimSpace(explicit); root != "" {
		return root, nil
	}
	return filepath.Join(os.TempDir(), "mini-go", "cache"), nil
}

func (b *DiskBackend) GetAction(id ActionID) (Entry, bool, error) {
	name, err := b.cachePath(actionDirectory, id.String())
	if err != nil {
		return Entry{}, false, err
	}
	data, err := os.ReadFile(name)
	if os.IsNotExist(err) {
		return Entry{}, false, nil
	}
	if err != nil {
		return Entry{}, false, err
	}
	entry, err := decodeEntry(id, data)
	if err != nil {
		return Entry{}, false, err
	}
	b.markUsed(name)
	return entry, true, nil
}

func (b *DiskBackend) GetOutput(id OutputID) ([]byte, bool, error) {
	name, err := b.cachePath(objectDirectory, id.String())
	if err != nil {
		return nil, false, err
	}
	data, err := os.ReadFile(name)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	decoded, err := decodeDiskOutput(data)
	if err != nil {
		return nil, false, err
	}
	if OutputIDFor(decoded) != id {
		return nil, false, errors.New("cache output does not match its content identifier")
	}
	b.markUsed(name)
	return decoded, true, nil
}

func (b *DiskBackend) PutOutput(id OutputID, data []byte) error {
	if OutputIDFor(data) != id {
		return errors.New("cache output does not match its content identifier")
	}
	name, err := b.cachePath(objectDirectory, id.String())
	if err != nil {
		return err
	}
	if current, readErr := os.ReadFile(name); readErr == nil {
		decoded, decodeErr := decodeDiskOutput(current)
		if decodeErr == nil && OutputIDFor(decoded) == id {
			b.markUsed(name)
			return nil
		}
	}
	encoded, err := encodeDiskOutput(data)
	if err != nil {
		return err
	}
	return atomicWrite(name, encoded)
}

func (b *DiskBackend) PutAction(id ActionID, entry Entry) error {
	if entry.Size < 0 {
		return errors.New("negative cache output size")
	}
	name, err := b.cachePath(actionDirectory, id.String())
	if err != nil {
		return err
	}
	data := []byte(fmt.Sprintf("%s %s %s %020d\n", entryVersion, id.String(), entry.Output.String(), entry.Size))
	return atomicWrite(name, data)
}

func (b *DiskBackend) Inspect() (DiskStats, error) {
	var stats DiskStats
	for _, directory := range []string{actionDirectory, objectDirectory} {
		records, err := b.records(directory)
		if err != nil {
			return DiskStats{}, err
		}
		for _, item := range records {
			stats.Bytes += item.size
			if directory == actionDirectory {
				stats.Actions++
			} else {
				stats.Objects++
			}
		}
	}
	return stats, nil
}

func (b *DiskBackend) Verify() error {
	actions, err := b.records(actionDirectory)
	if err != nil {
		return err
	}
	for _, item := range actions {
		actionID, err := ParseActionID(item.key)
		if err != nil {
			return fmt.Errorf("verify action %s: %w", item.key, err)
		}
		data, err := os.ReadFile(item.name)
		if err != nil {
			return err
		}
		entry, err := decodeEntry(actionID, data)
		if err != nil {
			return fmt.Errorf("verify action %s: %w", item.key, err)
		}
		output, found, err := b.GetOutput(entry.Output)
		if err != nil || !found || int64(len(output)) != entry.Size {
			return fmt.Errorf("verify action %s: output missing or corrupt", item.key)
		}
	}
	objects, err := b.records(objectDirectory)
	if err != nil {
		return err
	}
	for _, item := range objects {
		outputID, err := ParseOutputID(item.key)
		if err != nil {
			return fmt.Errorf("verify output %s: %w", item.key, err)
		}
		_, found, err := b.GetOutput(outputID)
		if err != nil || !found {
			return fmt.Errorf("verify output %s: content hash mismatch", item.key)
		}
	}
	return nil
}

func (b *DiskBackend) Clean() error {
	if strings.TrimSpace(b.root) == "" {
		return errors.New("missing cache root")
	}
	for _, name := range []string{actionDirectory, objectDirectory, "trim"} {
		if err := os.RemoveAll(filepath.Join(b.root, name)); err != nil {
			return err
		}
	}
	return nil
}

func (b *DiskBackend) Trim() error {
	if strings.TrimSpace(b.root) == "" {
		return errors.New("missing cache root")
	}
	now := b.now()
	marker := filepath.Join(b.root, "trim")
	if data, err := os.ReadFile(marker); err == nil {
		if unix, parseErr := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64); parseErr == nil {
			elapsed := now.Sub(time.Unix(unix, 0))
			if elapsed < trimInterval && elapsed > -mtimeInterval {
				return nil
			}
		}
	}
	if err := atomicWrite(marker, []byte(strconv.FormatInt(now.Unix(), 10)+"\n")); err != nil {
		return err
	}
	cutoff := now.Add(-trimLimit)
	for _, directory := range []string{actionDirectory, objectDirectory} {
		records, err := b.records(directory)
		if err != nil {
			return err
		}
		for _, item := range records {
			if !item.modTime.Before(cutoff) {
				continue
			}
			if err := os.Remove(item.name); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}

func (b *DiskBackend) records(directory string) ([]diskRecord, error) {
	if strings.TrimSpace(b.root) == "" {
		return nil, errors.New("missing cache root")
	}
	base := filepath.Join(b.root, directory)
	var records []diskRecord
	err := filepath.WalkDir(base, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".tmp-") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		records = append(records, diskRecord{name: name, key: entry.Name(), size: info.Size(), modTime: info.ModTime()})
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return records, nil
}

func (b *DiskBackend) markUsed(name string) {
	info, err := os.Stat(name)
	if err != nil {
		return
	}
	now := b.now()
	if now.Sub(info.ModTime()) >= mtimeInterval {
		_ = os.Chtimes(name, now, now)
	}
}

func (b *DiskBackend) cachePath(directory, key string) (string, error) {
	if strings.TrimSpace(b.root) == "" {
		return "", errors.New("missing cache root")
	}
	if directory != actionDirectory && directory != objectDirectory {
		return "", errors.New("invalid cache directory")
	}
	if len(key) != 64 || strings.ContainsAny(key, `/\\`) {
		return "", errors.New("invalid cache identifier")
	}
	return filepath.Join(b.root, directory, key[:2], key), nil
}

func atomicWrite(name string, data []byte) error {
	dir := filepath.Dir(name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Chmod(0o644); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, name)
}
