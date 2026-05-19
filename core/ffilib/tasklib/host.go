package tasklib

import (
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
)

func RegisterTaskAll(executor interface {
	RegisterFFISchema(string, ffigo.FFIBridge, uint32, *runtime.RuntimeFuncSig, string)
}) {
	executor.RegisterFFISchema("task.Sleep", nil, 0, runtime.MustParseRuntimeFuncSig("function(Int64) Void"), "")
	executor.RegisterFFISchema("task.Yield", nil, 0, runtime.MustParseRuntimeFuncSig("function() Void"), "")
}
