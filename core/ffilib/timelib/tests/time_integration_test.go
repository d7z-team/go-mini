package timelib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/internal/testutil"
)

func TestNowParseDurationAndConstants(t *testing.T) {
	testutil.Run(t, `
package main
import "time"

func main() {
	now := time.Now()
	if now.Unix() <= 0 {
		panic("time.Now failed")
	}

	d, err := time.ParseDuration("1h")
	if err != nil {
		panic(err)
	}
	if now.Add(d).Sub(now) != d {
		panic("time.Add/Sub failed")
	}
	if time.Second <= 0 {
		panic("time constant missing")
	}
}
`)
}
