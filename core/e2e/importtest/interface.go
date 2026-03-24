package importtest

import (
	"context"
	"time"
)

// ffigen:module tester
type ImportTester interface {
	Sleep(ctx context.Context, d time.Duration) error
}
