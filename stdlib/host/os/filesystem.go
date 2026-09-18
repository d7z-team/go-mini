package oshost

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"sync"
	"time"

	"github.com/d7z-team/mini-go/rpc"
)

// Filesystem is the host-neutral storage contract used by the os package.
type Filesystem interface {
	Open(name string, flag int, perm fs.FileMode) (File, error)
	Stat(name string) (fs.FileInfo, error)
	Lstat(name string) (fs.FileInfo, error)
	ReadDir(name string) ([]fs.DirEntry, error)
	Mkdir(name string, perm fs.FileMode) error
	Remove(name string) error
	Rename(oldName, newName string) error
	Readlink(name string) (string, error)
	Chtimes(name string, atime, mtime time.Time) error
	Getwd() (string, error)
	UserCacheDir() (string, error)
}

// File is the seekable directory-aware file shape required by the host adapter.
type File interface {
	io.Reader
	io.Writer
	io.ReaderAt
	io.WriterAt
	io.Seeker
	io.Closer
	Stat() (fs.FileInfo, error)
	ReadDir(count int) ([]fs.DirEntry, error)
}

type filesystemProviderBinding struct{ filesystemBackend Filesystem }

// NewFilesystemProvider exposes filesystem through the generated os RPC contract. The
// route owns exported file resources; the backend remains application-owned.
func NewFilesystemProvider(filesystemBackend Filesystem) (rpc.Provider, error) {
	if filesystemBackend == nil {
		return nil, errors.New("filesystem is required")
	}
	return NewOsFilesystemProvider(&filesystemProviderBinding{filesystemBackend: filesystemBackend})
}

func (binding *filesystemProviderBinding) Open(_ context.Context, name string, flag int64, perm uint32) (OsFileHandleHandler, OsOperationFault, error) {
	nativeFlag := int(flag)
	if int64(nativeFlag) != flag {
		return nil, operationFault(fs.ErrInvalid), nil
	}
	file, openErr := binding.filesystemBackend.Open(name, nativeFlag, fs.FileMode(perm))
	if openErr != nil {
		return nil, operationFault(openErr), nil
	}
	return &fileBinding{file: file}, OsOperationFault{}, nil
}

func (binding *filesystemProviderBinding) Stat(_ context.Context, name string, follow bool) (OsFileMetadata, OsOperationFault, error) {
	var info fs.FileInfo
	var err error
	if follow {
		info, err = binding.filesystemBackend.Stat(name)
	} else {
		info, err = binding.filesystemBackend.Lstat(name)
	}
	metadata, fault := fileInfoResult(info, err)
	return metadata, fault, nil
}

func (binding *filesystemProviderBinding) ReadDir(_ context.Context, name string) ([]OsFileMetadata, OsOperationFault, error) {
	entries, err := binding.filesystemBackend.ReadDir(name)
	if err != nil {
		return nil, operationFault(err), nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	metadata, err := directoryEntries(entries)
	return metadata, operationFault(err), nil
}

func (binding *filesystemProviderBinding) Mkdir(_ context.Context, name string, perm uint32) (OsOperationFault, error) {
	return operationFault(binding.filesystemBackend.Mkdir(name, fs.FileMode(perm))), nil
}

func (binding *filesystemProviderBinding) Remove(_ context.Context, name string) (OsOperationFault, error) {
	return operationFault(binding.filesystemBackend.Remove(name)), nil
}

func (binding *filesystemProviderBinding) Rename(_ context.Context, oldName, newName string) (OsOperationFault, error) {
	return operationFault(binding.filesystemBackend.Rename(oldName, newName)), nil
}

func (binding *filesystemProviderBinding) Readlink(_ context.Context, name string) (string, OsOperationFault, error) {
	value, err := binding.filesystemBackend.Readlink(name)
	return value, operationFault(err), nil
}

func (binding *filesystemProviderBinding) Chtimes(_ context.Context, name string, atimeSeconds, atimeNanoseconds, mtimeSeconds, mtimeNanoseconds int64) (OsOperationFault, error) {
	err := binding.filesystemBackend.Chtimes(name, time.Unix(atimeSeconds, atimeNanoseconds), time.Unix(mtimeSeconds, mtimeNanoseconds))
	return operationFault(err), nil
}

func (binding *filesystemProviderBinding) Getwd(context.Context) (string, OsOperationFault, error) {
	value, err := binding.filesystemBackend.Getwd()
	return value, operationFault(err), nil
}

func (binding *filesystemProviderBinding) UserCacheDir(context.Context) (string, OsOperationFault, error) {
	value, err := binding.filesystemBackend.UserCacheDir()
	return value, operationFault(err), nil
}

type fileBinding struct {
	mu   sync.Mutex
	file File
}

func (binding *fileBinding) Read(_ context.Context, size int64) ([]uint8, OsOperationFault, error) {
	binding.mu.Lock()
	defer binding.mu.Unlock()
	if binding.file == nil {
		return nil, operationFault(fs.ErrClosed), nil
	}
	if size < 0 || size > 64<<20 {
		return nil, operationFault(fs.ErrInvalid), nil
	}
	data := make([]byte, int(size))
	count, err := binding.file.Read(data)
	data = data[:count]
	if errors.Is(err, io.EOF) {
		err = nil
	}
	return data, operationFault(err), nil
}

func (binding *fileBinding) Write(_ context.Context, data []uint8) (int64, OsOperationFault, error) {
	binding.mu.Lock()
	defer binding.mu.Unlock()
	if binding.file == nil {
		return 0, operationFault(fs.ErrClosed), nil
	}
	count, err := binding.file.Write(data)
	return int64(count), operationFault(err), nil
}

func (binding *fileBinding) ReadAt(_ context.Context, size, offset int64) ([]uint8, OsOperationFault, error) {
	binding.mu.Lock()
	defer binding.mu.Unlock()
	if binding.file == nil {
		return nil, operationFault(fs.ErrClosed), nil
	}
	if size < 0 || size > 64<<20 || offset < 0 {
		return nil, operationFault(fs.ErrInvalid), nil
	}
	data := make([]byte, int(size))
	count, err := binding.file.ReadAt(data, offset)
	return data[:count], operationFault(err), nil
}

func (binding *fileBinding) WriteAt(_ context.Context, data []uint8, offset int64) (int64, OsOperationFault, error) {
	binding.mu.Lock()
	defer binding.mu.Unlock()
	if binding.file == nil {
		return 0, operationFault(fs.ErrClosed), nil
	}
	count, err := binding.file.WriteAt(data, offset)
	return int64(count), operationFault(err), nil
}

func (binding *fileBinding) Seek(_ context.Context, offset, whence int64) (int64, OsOperationFault, error) {
	binding.mu.Lock()
	defer binding.mu.Unlock()
	if binding.file == nil {
		return 0, operationFault(fs.ErrClosed), nil
	}
	nativeWhence := int(whence)
	if int64(nativeWhence) != whence {
		return 0, operationFault(fs.ErrInvalid), nil
	}
	position, err := binding.file.Seek(offset, nativeWhence)
	return position, operationFault(err), nil
}

func (binding *fileBinding) Stat(context.Context) (OsFileMetadata, OsOperationFault, error) {
	binding.mu.Lock()
	defer binding.mu.Unlock()
	if binding.file == nil {
		return OsFileMetadata{}, operationFault(fs.ErrClosed), nil
	}
	info, err := binding.file.Stat()
	metadata, fault := fileInfoResult(info, err)
	return metadata, fault, nil
}

func (binding *fileBinding) ReadDir(_ context.Context, count int64) ([]OsFileMetadata, OsOperationFault, error) {
	binding.mu.Lock()
	defer binding.mu.Unlock()
	if binding.file == nil {
		return nil, operationFault(fs.ErrClosed), nil
	}
	if count <= 0 {
		count = 0
	}
	nativeCount := int(count)
	if int64(nativeCount) != count {
		return nil, operationFault(fs.ErrInvalid), nil
	}
	entries, readErr := binding.file.ReadDir(nativeCount)
	metadata, metadataErr := directoryEntries(entries)
	if readErr == nil {
		readErr = metadataErr
	}
	return metadata, operationFault(readErr), nil
}

func (binding *fileBinding) Close(context.Context) error {
	if binding == nil {
		return nil
	}
	binding.mu.Lock()
	defer binding.mu.Unlock()
	if binding.file == nil {
		return nil
	}
	file := binding.file
	binding.file = nil
	return file.Close()
}

func fileInfoResult(info fs.FileInfo, err error) (OsFileMetadata, OsOperationFault) {
	if err != nil || info == nil {
		if err == nil {
			err = errors.New("filesystem returned nil file info")
		}
		return OsFileMetadata{}, operationFault(err)
	}
	return fileInfoValue(info), OsOperationFault{}
}

func directoryEntries(entries []fs.DirEntry) ([]OsFileMetadata, error) {
	values := make([]OsFileMetadata, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return values, err
		}
		values = append(values, fileInfoValue(info))
	}
	return values, nil
}

func fileInfoValue(info fs.FileInfo) OsFileMetadata {
	if info == nil {
		return OsFileMetadata{}
	}
	return OsFileMetadata{
		Name: info.Name(), Size: info.Size(), Mode: uint32(info.Mode()),
		ModifiedSeconds: info.ModTime().Unix(), ModifiedNanoseconds: int64(info.ModTime().Nanosecond()),
		Directory: info.IsDir(),
	}
}

func operationFault(err error) OsOperationFault {
	if err == nil {
		return OsOperationFault{}
	}
	code, message := operationError(err)
	return OsOperationFault{Code: code, Message: message}
}

func operationError(err error) (string, string) {
	switch {
	case errors.Is(err, errors.ErrUnsupported):
		return "unsupported", err.Error()
	case errors.Is(err, fs.ErrInvalid):
		return "invalid", err.Error()
	case errors.Is(err, fs.ErrPermission):
		return "permission", err.Error()
	case errors.Is(err, fs.ErrExist):
		return "exist", err.Error()
	case errors.Is(err, fs.ErrNotExist):
		return "not_exist", err.Error()
	case errors.Is(err, fs.ErrClosed):
		return "closed", err.Error()
	case errors.Is(err, io.EOF):
		return "eof", err.Error()
	default:
		return "io", fmt.Sprint(err)
	}
}
