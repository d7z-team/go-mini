package workspace

import "io/fs"

// StandardLibrary maps a flattened standard-library filesystem to compiler
// source packages with std::<import-path> identities.
func StandardLibrary(library fs.FS) (SourceSet, error) {
	return DiscoverIndexedPackageTree(library, ".")
}
