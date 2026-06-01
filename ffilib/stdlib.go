package ffilib

import (
	"gopkg.d7z.net/go-mini/core/surface"
	"gopkg.d7z.net/go-mini/ffilib/byteslib"
	"gopkg.d7z.net/go-mini/ffilib/contextlib"
	"gopkg.d7z.net/go-mini/ffilib/crypto/md5lib"
	"gopkg.d7z.net/go-mini/ffilib/crypto/sha256lib"
	"gopkg.d7z.net/go-mini/ffilib/encoding/base64lib"
	"gopkg.d7z.net/go-mini/ffilib/encoding/hexlib"
	"gopkg.d7z.net/go-mini/ffilib/filepathlib"
	"gopkg.d7z.net/go-mini/ffilib/imagelib"
	"gopkg.d7z.net/go-mini/ffilib/iolib"
	"gopkg.d7z.net/go-mini/ffilib/jsonlib"
	"gopkg.d7z.net/go-mini/ffilib/math/randlib"
	"gopkg.d7z.net/go-mini/ffilib/net/urllib"
	"gopkg.d7z.net/go-mini/ffilib/oslib"
	"gopkg.d7z.net/go-mini/ffilib/regexplib"
	"gopkg.d7z.net/go-mini/ffilib/synclib"
	"gopkg.d7z.net/go-mini/ffilib/timelib"
	"gopkg.d7z.net/go-mini/ffilib/unicode/utf8lib"
)

func Surface() *surface.Bundle {
	return surface.Merge(
		jsonlib.Surface(),
		timelib.SurfaceModule(&timelib.TimeHost{}),
		timelib.SurfaceTime(),
		contextlib.Surface(),
		filepathlib.SurfaceFilepath(&filepathlib.FilepathHost{}),
		byteslib.SurfaceBytes(&byteslib.BytesHost{}),
		regexplib.SurfaceRegexp(&regexplib.RegexpHost{}),
		randlib.SurfaceRand(randlib.NewRandHost()),
		utf8lib.SurfaceUTF8(&utf8lib.UTF8Host{}),
		synclib.SurfaceModule(&synclib.ModuleHost{}),
		synclib.SurfaceWaitGroup(),
		base64lib.Surface(),
		hexlib.SurfaceHex(&hexlib.HexHost{}),
		md5lib.SurfaceMD5(&md5lib.MD5Host{}),
		sha256lib.SurfaceSHA256(&sha256lib.SHA256Host{}),
		urllib.SurfaceURL(&urllib.URLHost{}),
		iolib.SurfaceIO(&iolib.IOHost{}),
		iolib.SurfaceReaderSchema(),
		iolib.SurfaceWriterSchema(),
		iolib.SurfaceFile(),
		imagelib.SurfaceImageLib(&imagelib.ImageHost{}),
		imagelib.SurfaceImage(),
		oslib.SurfaceOS(&oslib.OSHost{}),
	)
}
