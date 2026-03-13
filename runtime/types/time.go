package types

import (
	"time"

	"gopkg.d7z.net/go-mini/core/ast"
)

type MiniTime struct {
	t time.Time
}

func NewMiniTime(t time.Time) *MiniTime {
	return &MiniTime{t: t}
}

func (o *MiniTime) OPSType() ast.Ident {
	return "Time"
}

func (o *MiniTime) GoString() string {
	return o.t.Format(time.RFC3339)
}

func (o *MiniTime) String() ast.MiniString {
	return ast.NewMiniString(o.GoString())
}

func (o *MiniTime) Format(layout *ast.MiniString) ast.MiniString {
	return ast.NewMiniString(o.t.Format(layout.GoString()))
}

func (o *MiniTime) Unix() ast.MiniInt64 {
	return ast.NewMiniInt64(o.t.Unix())
}

func (o *MiniTime) UnixMilli() ast.MiniInt64 {
	return ast.NewMiniInt64(o.t.UnixMilli())
}

func (o *MiniTime) Add(seconds *ast.MiniInt64) *MiniTime {
	return NewMiniTime(o.t.Add(time.Duration(seconds.GoValue().(int64)) * time.Second))
}

func (o *MiniTime) AddDate(years, months, days *ast.MiniInt64) *MiniTime {
	return NewMiniTime(o.t.AddDate(
		int(years.GoValue().(int64)),
		int(months.GoValue().(int64)),
		int(days.GoValue().(int64)),
	))
}

func (o *MiniTime) Sub(other *MiniTime) ast.MiniInt64 {
	return ast.NewMiniInt64(int64(o.t.Sub(other.t).Seconds()))
}

func (o *MiniTime) Before(other *MiniTime) ast.MiniBool {
	return ast.NewMiniBool(o.t.Before(other.t))
}

func (o *MiniTime) After(other *MiniTime) ast.MiniBool {
	return ast.NewMiniBool(o.t.After(other.t))
}

func (o *MiniTime) IsZero() ast.MiniBool {
	return ast.NewMiniBool(o.t.IsZero())
}

func (o *MiniTime) Year() ast.MiniInt64 {
	return ast.NewMiniInt64(int64(o.t.Year()))
}

func (o *MiniTime) Month() ast.MiniInt64 {
	return ast.NewMiniInt64(int64(o.t.Month()))
}

func (o *MiniTime) Day() ast.MiniInt64 {
	return ast.NewMiniInt64(int64(o.t.Day()))
}

func (o *MiniTime) Hour() ast.MiniInt64 {
	return ast.NewMiniInt64(int64(o.t.Hour()))
}

func (o *MiniTime) Minute() ast.MiniInt64 {
	return ast.NewMiniInt64(int64(o.t.Minute()))
}

func (o *MiniTime) Second() ast.MiniInt64 {
	return ast.NewMiniInt64(int64(o.t.Second()))
}
