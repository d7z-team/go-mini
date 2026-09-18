package oshost

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
)

// MemoryFilesystem is a deterministic writable filesystem for tests and
// sandboxed embeddings. Paths use io/fs slash-separated relative syntax.
type MemoryFilesystem struct {
	mu    sync.RWMutex
	nodes map[string]*memoryNode
}

type memoryNode struct {
	data     []byte
	mode     fs.FileMode
	modified time.Time
}

func NewMemoryFilesystem(files map[string][]byte) (*MemoryFilesystem, error) {
	filesystem := &MemoryFilesystem{nodes: map[string]*memoryNode{}}
	for name, data := range files {
		if err := filesystem.addFile(name, data, 0o666); err != nil {
			return nil, err
		}
	}
	return filesystem, nil
}

func (filesystem *MemoryFilesystem) addFile(name string, data []byte, mode fs.FileMode) error {
	name, err := memoryPath(name)
	if err != nil || name == "." {
		return fs.ErrInvalid
	}
	for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
		if node := filesystem.nodes[parent]; node != nil && !node.mode.IsDir() {
			return fs.ErrInvalid
		}
		if filesystem.nodes[parent] == nil {
			filesystem.nodes[parent] = &memoryNode{mode: fs.ModeDir | 0o777}
		}
	}
	filesystem.nodes[name] = &memoryNode{data: append([]byte(nil), data...), mode: mode.Perm(), modified: time.Unix(0, 0)}
	return nil
}

func (filesystem *MemoryFilesystem) Open(name string, flag int, perm fs.FileMode) (File, error) {
	name, err := memoryPath(name)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: err}
	}
	filesystem.mu.Lock()
	defer filesystem.mu.Unlock()
	node := filesystem.node(name)
	if node == nil {
		if flag&os.O_CREATE == 0 || name == "." {
			return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
		}
		parent := filesystem.node(path.Dir(name))
		if parent == nil || !parent.mode.IsDir() {
			return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
		}
		node = &memoryNode{mode: perm.Perm(), modified: time.Unix(0, 0)}
		filesystem.nodes[name] = node
	} else if flag&os.O_CREATE != 0 && flag&os.O_EXCL != 0 {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrExist}
	}
	if node.mode.IsDir() && flag&3 != os.O_RDONLY {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
	}
	if flag&os.O_TRUNC != 0 && flag&3 != os.O_RDONLY && !node.mode.IsDir() {
		node.data = nil
	}
	offset := int64(0)
	if flag&os.O_APPEND != 0 {
		offset = int64(len(node.data))
	}
	return &memoryFile{filesystem: filesystem, name: name, flag: flag, offset: offset}, nil
}

func (filesystem *MemoryFilesystem) Stat(name string) (fs.FileInfo, error) {
	return filesystem.Lstat(name)
}

func (filesystem *MemoryFilesystem) Lstat(name string) (fs.FileInfo, error) {
	name, err := memoryPath(name)
	if err != nil {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: err}
	}
	filesystem.mu.RLock()
	defer filesystem.mu.RUnlock()
	node := filesystem.node(name)
	if node == nil {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
	}
	return memoryInfoFor(name, node), nil
}

func (filesystem *MemoryFilesystem) ReadDir(name string) ([]fs.DirEntry, error) {
	name, err := memoryPath(name)
	if err != nil {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: err}
	}
	filesystem.mu.RLock()
	defer filesystem.mu.RUnlock()
	node := filesystem.node(name)
	if node == nil || !node.mode.IsDir() {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	prefix := ""
	if name != "." {
		prefix = name + "/"
	}
	var entries []fs.DirEntry
	for candidate, child := range filesystem.nodes {
		if !strings.HasPrefix(candidate, prefix) {
			continue
		}
		remainder := strings.TrimPrefix(candidate, prefix)
		if remainder == "" || strings.Contains(remainder, "/") {
			continue
		}
		entries = append(entries, memoryEntry{info: memoryInfoFor(candidate, child)})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries, nil
}

func (filesystem *MemoryFilesystem) Mkdir(name string, perm fs.FileMode) error {
	name, err := memoryPath(name)
	if err != nil || name == "." {
		return &fs.PathError{Op: "mkdir", Path: name, Err: fs.ErrInvalid}
	}
	filesystem.mu.Lock()
	defer filesystem.mu.Unlock()
	if filesystem.nodes[name] != nil {
		return &fs.PathError{Op: "mkdir", Path: name, Err: fs.ErrExist}
	}
	parent := filesystem.node(path.Dir(name))
	if parent == nil || !parent.mode.IsDir() {
		return &fs.PathError{Op: "mkdir", Path: name, Err: fs.ErrNotExist}
	}
	filesystem.nodes[name] = &memoryNode{mode: fs.ModeDir | perm.Perm(), modified: time.Unix(0, 0)}
	return nil
}

func (filesystem *MemoryFilesystem) Remove(name string) error {
	name, err := memoryPath(name)
	if err != nil || name == "." {
		return &fs.PathError{Op: "remove", Path: name, Err: fs.ErrInvalid}
	}
	filesystem.mu.Lock()
	defer filesystem.mu.Unlock()
	if filesystem.nodes[name] == nil {
		return &fs.PathError{Op: "remove", Path: name, Err: fs.ErrNotExist}
	}
	prefix := name + "/"
	for candidate := range filesystem.nodes {
		if strings.HasPrefix(candidate, prefix) {
			return &fs.PathError{Op: "remove", Path: name, Err: errors.New("directory not empty")}
		}
	}
	delete(filesystem.nodes, name)
	return nil
}

func (filesystem *MemoryFilesystem) Rename(oldName, newName string) error {
	oldName, oldErr := memoryPath(oldName)
	newName, newErr := memoryPath(newName)
	if oldErr != nil || newErr != nil || oldName == "." || newName == "." || strings.HasPrefix(newName, oldName+"/") {
		return fs.ErrInvalid
	}
	filesystem.mu.Lock()
	defer filesystem.mu.Unlock()
	if filesystem.nodes[oldName] == nil {
		return fs.ErrNotExist
	}
	parent := filesystem.node(path.Dir(newName))
	if parent == nil || !parent.mode.IsDir() {
		return fs.ErrNotExist
	}
	if filesystem.nodes[newName] != nil {
		return fs.ErrExist
	}
	moved := map[string]*memoryNode{}
	for candidate, node := range filesystem.nodes {
		if candidate == oldName || strings.HasPrefix(candidate, oldName+"/") {
			moved[newName+strings.TrimPrefix(candidate, oldName)] = node
			delete(filesystem.nodes, candidate)
		}
	}
	for candidate, node := range moved {
		filesystem.nodes[candidate] = node
	}
	return nil
}

func (*MemoryFilesystem) Readlink(string) (string, error) { return "", fs.ErrInvalid }

func (filesystem *MemoryFilesystem) Chtimes(name string, _, mtime time.Time) error {
	name, err := memoryPath(name)
	if err != nil {
		return err
	}
	filesystem.mu.Lock()
	defer filesystem.mu.Unlock()
	node := filesystem.node(name)
	if node == nil {
		return fs.ErrNotExist
	}
	node.modified = mtime
	return nil
}

func (*MemoryFilesystem) Getwd() (string, error)        { return ".", nil }
func (*MemoryFilesystem) UserCacheDir() (string, error) { return ".cache", nil }

func (filesystem *MemoryFilesystem) node(name string) *memoryNode {
	if name == "." {
		return &memoryNode{mode: fs.ModeDir | 0o777, modified: time.Unix(0, 0)}
	}
	return filesystem.nodes[name]
}

type memoryFile struct {
	filesystem *MemoryFilesystem
	name       string
	flag       int
	offset     int64
	closed     bool
	dirOffset  int
}

func (file *memoryFile) Read(buffer []byte) (int, error) {
	file.filesystem.mu.Lock()
	defer file.filesystem.mu.Unlock()
	count, err := file.readAt(buffer, file.offset)
	file.offset += int64(count)
	if count > 0 && err == io.EOF {
		err = nil
	}
	return count, err
}

func (file *memoryFile) ReadAt(buffer []byte, offset int64) (int, error) {
	file.filesystem.mu.RLock()
	defer file.filesystem.mu.RUnlock()
	return file.readAt(buffer, offset)
}

// readAt and writeAt require the filesystem lock and leave the file cursor unchanged.
func (file *memoryFile) readAt(buffer []byte, offset int64) (int, error) {
	if file.closed {
		return 0, fs.ErrClosed
	}
	if offset < 0 {
		return 0, fs.ErrInvalid
	}
	node := file.filesystem.node(file.name)
	if node == nil {
		return 0, fs.ErrNotExist
	}
	if node.mode.IsDir() || file.flag&3 == os.O_WRONLY {
		return 0, fs.ErrPermission
	}
	if len(buffer) == 0 {
		return 0, nil
	}
	if offset >= int64(len(node.data)) {
		return 0, io.EOF
	}
	count := copy(buffer, node.data[offset:])
	if count < len(buffer) {
		return count, io.EOF
	}
	return count, nil
}

func (file *memoryFile) Write(data []byte) (int, error) {
	file.filesystem.mu.Lock()
	defer file.filesystem.mu.Unlock()
	offset := file.offset
	if node := file.filesystem.node(file.name); node != nil && file.flag&os.O_APPEND != 0 {
		offset = int64(len(node.data))
	}
	count, err := file.writeAt(data, offset)
	if err == nil {
		file.offset = offset + int64(count)
	}
	return count, err
}

func (file *memoryFile) WriteAt(data []byte, offset int64) (int, error) {
	file.filesystem.mu.Lock()
	defer file.filesystem.mu.Unlock()
	if file.flag&os.O_APPEND != 0 {
		return 0, errors.New("os: invalid use of WriteAt on file opened with O_APPEND")
	}
	return file.writeAt(data, offset)
}

func (file *memoryFile) writeAt(data []byte, offset int64) (int, error) {
	if file.closed {
		return 0, fs.ErrClosed
	}
	if offset < 0 || offset > int64(int(^uint(0)>>1))-int64(len(data)) {
		return 0, fs.ErrInvalid
	}
	node := file.filesystem.node(file.name)
	if node == nil {
		return 0, fs.ErrNotExist
	}
	if node.mode.IsDir() || file.flag&3 == os.O_RDONLY {
		return 0, fs.ErrPermission
	}
	if len(data) == 0 {
		return 0, nil
	}
	end := offset + int64(len(data))
	if end > int64(len(node.data)) {
		node.data = append(node.data, make([]byte, int(end)-len(node.data))...)
	}
	copy(node.data[offset:end], data)
	return len(data), nil
}

func (file *memoryFile) Seek(offset int64, whence int) (int64, error) {
	file.filesystem.mu.Lock()
	defer file.filesystem.mu.Unlock()
	if file.closed {
		return 0, fs.ErrClosed
	}
	node := file.filesystem.node(file.name)
	if node == nil || node.mode.IsDir() {
		return 0, fs.ErrInvalid
	}
	position := offset
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		position += file.offset
	case io.SeekEnd:
		position += int64(len(node.data))
	default:
		return 0, fs.ErrInvalid
	}
	if position < 0 {
		return 0, fs.ErrInvalid
	}
	file.offset = position
	return position, nil
}

func (file *memoryFile) Close() error {
	file.filesystem.mu.Lock()
	defer file.filesystem.mu.Unlock()
	if file.closed {
		return fs.ErrClosed
	}
	file.closed = true
	return nil
}

func (file *memoryFile) Stat() (fs.FileInfo, error) {
	file.filesystem.mu.RLock()
	defer file.filesystem.mu.RUnlock()
	if file.closed {
		return nil, fs.ErrClosed
	}
	node := file.filesystem.node(file.name)
	if node == nil {
		return nil, fs.ErrNotExist
	}
	return memoryInfoFor(file.name, node), nil
}

func (file *memoryFile) ReadDir(count int) ([]fs.DirEntry, error) {
	file.filesystem.mu.RLock()
	if file.closed {
		file.filesystem.mu.RUnlock()
		return nil, fs.ErrClosed
	}
	node := file.filesystem.node(file.name)
	file.filesystem.mu.RUnlock()
	if node == nil || !node.mode.IsDir() {
		return nil, fs.ErrInvalid
	}
	entries, err := file.filesystem.ReadDir(file.name)
	if err != nil {
		return nil, err
	}
	file.filesystem.mu.Lock()
	defer file.filesystem.mu.Unlock()
	if file.dirOffset >= len(entries) {
		if count > 0 {
			return []fs.DirEntry{}, io.EOF
		}
		return []fs.DirEntry{}, nil
	}
	end := len(entries)
	if count > 0 && file.dirOffset+count < end {
		end = file.dirOffset + count
	}
	out := append([]fs.DirEntry(nil), entries[file.dirOffset:end]...)
	file.dirOffset = end
	return out, nil
}

type memoryInfo struct {
	name     string
	size     int64
	mode     fs.FileMode
	modified time.Time
}

func memoryInfoFor(name string, node *memoryNode) memoryInfo {
	size := int64(len(node.data))
	if node.mode.IsDir() {
		size = 0
	}
	return memoryInfo{name: path.Base(name), size: size, mode: node.mode, modified: node.modified}
}

func (info memoryInfo) Name() string       { return info.name }
func (info memoryInfo) Size() int64        { return info.size }
func (info memoryInfo) Mode() fs.FileMode  { return info.mode }
func (info memoryInfo) ModTime() time.Time { return info.modified }
func (info memoryInfo) IsDir() bool        { return info.mode.IsDir() }
func (info memoryInfo) Sys() any           { return nil }

type memoryEntry struct{ info memoryInfo }

func (entry memoryEntry) Name() string               { return entry.info.Name() }
func (entry memoryEntry) IsDir() bool                { return entry.info.IsDir() }
func (entry memoryEntry) Type() fs.FileMode          { return entry.info.Mode().Type() }
func (entry memoryEntry) Info() (fs.FileInfo, error) { return entry.info, nil }

func memoryPath(name string) (string, error) {
	name = path.Clean(name)
	if name != "." && !fs.ValidPath(name) {
		return name, fs.ErrInvalid
	}
	return name, nil
}
