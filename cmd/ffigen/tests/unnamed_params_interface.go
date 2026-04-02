package tests

//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg tests -out unnamed_params_ffigen.go unnamed_params_interface.go

import "context"

// ffigen:module logger
type Logger interface {
	Log(ctx context.Context, msg, level string, code int64)
	// Internal uses unnamed parameters to test ffigen's default naming (arg0, arg1, etc.)
	Internal(string, string, int64)
}

// ffigen:module callback
// ffigen:reverse
type Callback interface {
	OnEvent(int64, string)
	// OnRaw uses unnamed parameters in a reverse proxy
	OnRaw(int64, []byte)
}
