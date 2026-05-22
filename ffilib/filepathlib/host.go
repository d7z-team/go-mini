package filepathlib

import (
	"path/filepath"
)

type FilepathHost struct{}

func (h *FilepathHost) Base(path string) string    { return filepath.Base(path) }
func (h *FilepathHost) Clean(path string) string   { return filepath.Clean(path) }
func (h *FilepathHost) Dir(path string) string     { return filepath.Dir(path) }
func (h *FilepathHost) Ext(path string) string     { return filepath.Ext(path) }
func (h *FilepathHost) IsAbs(path string) bool     { return filepath.IsAbs(path) }
func (h *FilepathHost) Join(elem ...string) string { return filepath.Join(elem...) }
func (h *FilepathHost) Match(pattern, name string) (bool, error) {
	return filepath.Match(pattern, name)
}

func (h *FilepathHost) Rel(basepath, targpath string) (string, error) {
	return filepath.Rel(basepath, targpath)
}
func (h *FilepathHost) Split(path string) (string, string) { return filepath.Split(path) }
func (h *FilepathHost) ToSlash(path string) string         { return filepath.ToSlash(path) }
func (h *FilepathHost) FromSlash(path string) string       { return filepath.FromSlash(path) }
func (h *FilepathHost) VolumeName(path string) string      { return filepath.VolumeName(path) }
