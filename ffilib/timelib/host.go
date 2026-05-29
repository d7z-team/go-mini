package timelib

import (
	"context"
	"sync"
	"time"

	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
)

// TimeHost 实现 Module 接口
type TimeHost struct{}

func (h *TimeHost) Unix(sec, nsec int64) *Time {
	return &Time{T: time.Unix(sec, nsec)}
}

func (h *TimeHost) NowContext(ctx context.Context) *Time {
	_ = ctx
	return &Time{T: time.Now()}
}

func (h *TimeHost) Now(ctx context.Context) *Time {
	return h.NowContext(ctx)
}

func (h *TimeHost) Sleep(ctx context.Context, ns int64) ffigo.Async[ffigo.Void] {
	if ns <= 0 {
		return ffigo.AsyncFunc[ffigo.Void](func(_ context.Context, done ffigo.Completion[ffigo.Void]) (ffigo.WaitHandle, error) {
			done.Complete(ffigo.Void{}, nil)
			return nil, nil
		})
	}
	service := runtime.VMTimerServiceFromContext(ctx)
	if service == nil {
		return ffigo.AsyncFunc[ffigo.Void](func(_ context.Context, done ffigo.Completion[ffigo.Void]) (ffigo.WaitHandle, error) {
			var once sync.Once
			finish := func(err error) {
				once.Do(func() {
					done.Complete(ffigo.Void{}, err)
				})
			}
			hostTimer := time.AfterFunc(time.Duration(ns), func() {
				finish(nil)
			})
			cancel := func() {
				hostTimer.Stop()
				finish(nil)
			}
			return ffigo.NewWaitHandle(ffigo.WaitExternal, "time.Sleep", cancel), nil
		})
	}
	timer := runtime.NewVMTimer(service, time.Duration(ns))
	return ffigo.AsyncFunc[ffigo.Void](func(waitCtx context.Context, done ffigo.Completion[ffigo.Void]) (ffigo.WaitHandle, error) {
		return timer.Wait().Start(waitCtx, voidCompletion{done: done})
	})
}

func (h *TimeHost) Since(ctx context.Context, t *Time) int64 {
	_ = ctx
	if t == nil {
		return 0
	}
	return int64(time.Since(t.T))
}

func (h *TimeHost) Until(ctx context.Context, t *Time) int64 {
	_ = ctx
	if t == nil {
		return 0
	}
	return int64(time.Until(t.T))
}

func (h *TimeHost) Parse(layout, value string) (*Time, error) {
	t, err := time.Parse(layout, value)
	if err != nil {
		return nil, err
	}
	return &Time{T: t}, nil
}

func (h *TimeHost) ParseDuration(s string) (int64, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	return int64(d), nil
}

// Time 是时间句柄
// ffigen:module time
// ffigen:methods
type Time struct {
	T time.Time
}

func (t *Time) Year() int64 {
	if t == nil {
		return 0
	}
	return int64(t.T.Year())
}

type voidCompletion struct {
	done ffigo.Completion[ffigo.Void]
}

func (c voidCompletion) Complete(_ bool, err error) bool {
	if c.done == nil {
		return false
	}
	return c.done.Complete(ffigo.Void{}, err)
}

func (t *Time) Month() int64 {
	if t == nil {
		return 0
	}
	return int64(t.T.Month())
}

func (t *Time) Day() int64 {
	if t == nil {
		return 0
	}
	return int64(t.T.Day())
}

func (t *Time) Hour() int64 {
	if t == nil {
		return 0
	}
	return int64(t.T.Hour())
}

func (t *Time) Minute() int64 {
	if t == nil {
		return 0
	}
	return int64(t.T.Minute())
}

func (t *Time) Second() int64 {
	if t == nil {
		return 0
	}
	return int64(t.T.Second())
}

func (t *Time) Nanosecond() int64 {
	if t == nil {
		return 0
	}
	return int64(t.T.Nanosecond())
}

func (t *Time) Unix() int64 {
	if t == nil {
		return 0
	}
	return t.T.Unix()
}

func (t *Time) UnixMilli() int64 {
	if t == nil {
		return 0
	}
	return t.T.UnixMilli()
}

func (t *Time) UnixMicro() int64 {
	if t == nil {
		return 0
	}
	return t.T.UnixMicro()
}

func (t *Time) UnixNano() int64 {
	if t == nil {
		return 0
	}
	return t.T.UnixNano()
}

func (t *Time) Format(layout string) string {
	if t == nil {
		return ""
	}
	return t.T.Format(layout)
}

func (t *Time) Add(d int64) *Time {
	if t == nil {
		return nil
	}
	return &Time{T: t.T.Add(time.Duration(d))}
}

func (t *Time) Sub(other *Time) int64 {
	if t == nil || other == nil {
		return 0
	}
	return int64(t.T.Sub(other.T))
}

func (t *Time) IsZero() bool {
	if t == nil {
		return true
	}
	return t.T.IsZero()
}

func (t *Time) Before(other *Time) bool {
	if t == nil || other == nil {
		return false
	}
	return t.T.Before(other.T)
}

func (t *Time) After(other *Time) bool {
	if t == nil || other == nil {
		return false
	}
	return t.T.After(other.T)
}

func (t *Time) Equal(other *Time) bool {
	if t == nil || other == nil {
		return false
	}
	return t.T.Equal(other.T)
}

func (t *Time) String() string {
	if t == nil {
		return ""
	}
	return t.T.String()
}
