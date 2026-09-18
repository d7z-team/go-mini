//go:build minigo

package compilerentry

import (
	"context"
	"encoding/json"
	"errors"
	"ffi"
	"strconv"
	"time"
)

func toolRequestContext(token, deadline string) (context.Context, func(), error) {
	if token == "" {
		return context.Background(), func() {}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	if deadline != "" {
		nanos, err := strconv.ParseInt(deadline, 10, 64)
		if err != nil {
			cancel()
			return nil, nil, err
		}
		cancel()
		ctx, cancel = context.WithDeadline(context.Background(), time.Unix(0, nanos))
	}
	stopped := make(chan struct{})
	wait, _ := json.Marshal(struct {
		Operation string
		Token     string
	}{"wait", token})
	go func() {
		result, err := ffi.Call("minigo.tools.control", wait)
		if err != nil || string(result) == "canceled" {
			cancel()
		}
		close(stopped)
	}()
	finish := func() {
		done, _ := json.Marshal(struct {
			Operation string
			Token     string
		}{"finish", token})
		_, err := ffi.Call("minigo.tools.control", done)
		cancel()
		if err != nil {
			panic(errors.New("compiler request control failed"))
		}
		<-stopped
	}
	return ctx, finish, nil
}
