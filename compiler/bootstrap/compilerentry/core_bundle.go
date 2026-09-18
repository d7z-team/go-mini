package compilerentry

import (
	"encoding/binary"
	"errors"
	"fmt"
)

var coreBundleMagic = []byte("mini-go-core\x02")

// EncodeCoreBundle encodes the compiler's embedded core packages. The bundle
// is generated build data rather than a distributed artifact format.
func EncodeCoreBundle(packages []Package) ([]byte, error) {
	out := append([]byte(nil), coreBundleMagic...)
	var err error
	out, err = appendBundleCount(out, len(packages))
	if err != nil {
		return nil, err
	}
	for _, pkg := range packages {
		for _, text := range []string{pkg.Namespace, pkg.PackagePath, pkg.ModulePath} {
			out, err = appendBundleBytes(out, []byte(text))
			if err != nil {
				return nil, err
			}
		}
		out, err = appendBundleCount(out, len(pkg.Files))
		if err != nil {
			return nil, err
		}
		for _, file := range pkg.Files {
			out, err = appendBundleBytes(out, []byte(file.Path))
			if err == nil {
				out, err = appendBundleBytes(out, []byte(file.Text))
			}
			if err != nil {
				return nil, err
			}
		}
		out, err = appendBundleCount(out, len(pkg.TestFiles))
		if err != nil {
			return nil, err
		}
		for _, file := range pkg.TestFiles {
			out, err = appendBundleBytes(out, []byte(file.Path))
			if err == nil {
				out, err = appendBundleBytes(out, []byte(file.Text))
			}
			if err != nil {
				return nil, err
			}
		}
		out, err = appendBundleCount(out, len(pkg.Resources))
		if err != nil {
			return nil, err
		}
		for _, resource := range pkg.Resources {
			out, err = appendBundleBytes(out, []byte(resource.Path))
			if err == nil {
				out, err = appendBundleBytes(out, []byte(resource.SourcePath))
			}
			if err == nil {
				out, err = appendBundleOptionalBytes(out, resource.Data)
			}
			if err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func appendBundleCount(out []byte, count int) ([]byte, error) {
	if count < 0 || uint64(count) > uint64(^uint32(0)) {
		return nil, errors.New("core bundle value is too large")
	}
	var encoded [4]byte
	binary.BigEndian.PutUint32(encoded[:], uint32(count))
	return append(out, encoded[:]...), nil
}

func appendBundleBytes(out, data []byte) ([]byte, error) {
	var err error
	out, err = appendBundleCount(out, len(data))
	if err != nil {
		return nil, err
	}
	return append(out, data...), nil
}

func appendBundleOptionalBytes(out, data []byte) ([]byte, error) {
	if data == nil {
		return append(out, 0xff, 0xff, 0xff, 0xff), nil
	}
	if uint64(len(data)) >= uint64(^uint32(0)) {
		return nil, errors.New("core bundle value is too large")
	}
	return appendBundleBytes(out, data)
}

func mustDecodeCoreBundle(data []byte) []Package {
	packages, err := DecodeCoreBundle(data)
	if err != nil {
		panic(err.Error())
	}
	return packages
}

// DecodeCoreBundle validates and decodes generated compiler core data.
func DecodeCoreBundle(data []byte) ([]Package, error) {
	if len(data) < len(coreBundleMagic) || string(data[:len(coreBundleMagic)]) != string(coreBundleMagic) {
		return nil, errors.New("invalid compiler core bundle header")
	}
	reader := coreBundleReader{data: data, offset: len(coreBundleMagic)}
	packageCount, err := reader.readCount("package")
	if err != nil {
		return nil, err
	}
	packages := make([]Package, packageCount)
	for packageIndex := range packages {
		pkg := &packages[packageIndex]
		if pkg.Namespace, err = reader.readText("namespace"); err != nil {
			return nil, err
		}
		if pkg.PackagePath, err = reader.readText("package path"); err != nil {
			return nil, err
		}
		if pkg.ModulePath, err = reader.readText("module path"); err != nil {
			return nil, err
		}
		fileCount, countErr := reader.readCount("file")
		if countErr != nil {
			return nil, countErr
		}
		pkg.Files = make([]File, fileCount)
		for fileIndex := range pkg.Files {
			if pkg.Files[fileIndex].Path, err = reader.readText("file path"); err != nil {
				return nil, err
			}
			if pkg.Files[fileIndex].Text, err = reader.readText("file text"); err != nil {
				return nil, err
			}
		}
		testFileCount, countErr := reader.readCount("test file")
		if countErr != nil {
			return nil, countErr
		}
		pkg.TestFiles = make([]File, testFileCount)
		for fileIndex := range pkg.TestFiles {
			if pkg.TestFiles[fileIndex].Path, err = reader.readText("test file path"); err != nil {
				return nil, err
			}
			if pkg.TestFiles[fileIndex].Text, err = reader.readText("test file text"); err != nil {
				return nil, err
			}
		}
		resourceCount, countErr := reader.readCount("resource")
		if countErr != nil {
			return nil, countErr
		}
		pkg.Resources = make([]Resource, resourceCount)
		for resourceIndex := range pkg.Resources {
			resource := &pkg.Resources[resourceIndex]
			if resource.Path, err = reader.readText("resource path"); err != nil {
				return nil, err
			}
			if resource.SourcePath, err = reader.readText("resource source path"); err != nil {
				return nil, err
			}
			if resource.Data, err = reader.readOptionalBytes("resource data"); err != nil {
				return nil, err
			}
		}
	}
	if reader.offset != len(reader.data) {
		return nil, errors.New("compiler core bundle has trailing data")
	}
	return packages, nil
}

type coreBundleReader struct {
	data   []byte
	offset int
}

func (reader *coreBundleReader) readCount(kind string) (int, error) {
	if reader.offset > len(reader.data)-4 {
		return 0, fmt.Errorf("truncated compiler core bundle %s count", kind)
	}
	count := uint32(reader.data[reader.offset])<<24 |
		uint32(reader.data[reader.offset+1])<<16 |
		uint32(reader.data[reader.offset+2])<<8 |
		uint32(reader.data[reader.offset+3])
	reader.offset += 4
	if uint64(count) > uint64(len(reader.data)-reader.offset) {
		return 0, fmt.Errorf("invalid compiler core bundle %s count %d at offset %d of %d", kind, count, reader.offset-4, len(reader.data))
	}
	return int(count), nil
}

func (reader *coreBundleReader) readBytes(kind string) ([]byte, error) {
	length, err := reader.readCount(kind + " byte")
	if err != nil {
		return nil, err
	}
	if length > len(reader.data)-reader.offset {
		return nil, fmt.Errorf("truncated compiler core bundle %s", kind)
	}
	value := reader.data[reader.offset : reader.offset+length]
	reader.offset += length
	return value, nil
}

func (reader *coreBundleReader) readOptionalBytes(kind string) ([]byte, error) {
	if reader.offset > len(reader.data)-4 {
		return nil, fmt.Errorf("truncated compiler core bundle %s byte count", kind)
	}
	if reader.data[reader.offset] == 0xff && reader.data[reader.offset+1] == 0xff &&
		reader.data[reader.offset+2] == 0xff && reader.data[reader.offset+3] == 0xff {
		reader.offset += 4
		return nil, nil
	}
	return reader.readBytes(kind)
}

func (reader *coreBundleReader) readText(kind string) (string, error) {
	value, err := reader.readBytes(kind)
	return string(value), err
}
