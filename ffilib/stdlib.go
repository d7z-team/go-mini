package ffilib

import (
	"gopkg.d7z.net/go-mini/core/calltemplate"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/ffilib/byteslib"
	"gopkg.d7z.net/go-mini/ffilib/crypto/md5lib"
	"gopkg.d7z.net/go-mini/ffilib/crypto/sha256lib"
	"gopkg.d7z.net/go-mini/ffilib/encoding/base64lib"
	"gopkg.d7z.net/go-mini/ffilib/encoding/hexlib"
	"gopkg.d7z.net/go-mini/ffilib/filepathlib"
	"gopkg.d7z.net/go-mini/ffilib/fmtlib"
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

type Registrar interface {
	RegisterFFISchema(string, ffigo.FFIBridge, uint32, *runtime.RuntimeFuncSig, string)
	RegisterStructSchema(string, *runtime.RuntimeStructSpec)
	RegisterInterfaceSchema(string, *runtime.RuntimeInterfaceSpec)
	RegisterConstant(string, string)
	RegisterFunctionTemplate(calltemplate.FunctionTemplate) error
	HandleRegistry() *ffigo.HandleRegistry
}

func RegisterAll(executor Registrar) {
	registry := executor.HandleRegistry()
	jsonlib.RegisterJSON(executor, &jsonlib.JSONHost{}, registry)
	timelib.RegisterTimeAll(executor, &timelib.TimeHost{}, registry)
	filepathlib.RegisterFilepath(executor, &filepathlib.FilepathHost{}, registry)
	byteslib.RegisterBytes(executor, &byteslib.BytesHost{}, registry)
	regexplib.RegisterRegexp(executor, &regexplib.RegexpHost{}, registry)
	randlib.RegisterRand(executor, randlib.NewRandHost(), registry)
	utf8lib.RegisterUTF8(executor, &utf8lib.UTF8Host{}, registry)
	synclib.RegisterSyncAll(executor, &synclib.ModuleHost{}, registry)
	base64lib.RegisterBase64(executor, &base64lib.Base64Host{}, registry)
	hexlib.RegisterHex(executor, &hexlib.HexHost{}, registry)
	md5lib.RegisterMD5(executor, &md5lib.MD5Host{}, registry)
	sha256lib.RegisterSHA256(executor, &sha256lib.SHA256Host{}, registry)
	urllib.RegisterURL(executor, &urllib.URLHost{}, registry)
	iolib.RegisterIOSafe(executor, &iolib.IOHost{}, registry)
	iolib.RegisterFile(executor, registry)
	imagelib.RegisterImageAll(executor, &imagelib.ImageHost{}, registry)
	oslib.RegisterOS(executor, &oslib.OSHost{}, registry)
	fmtlib.RegisterFmtAll(executor, &fmtlib.FmtHost{}, registry)
}
