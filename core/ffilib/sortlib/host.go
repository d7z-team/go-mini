package sortlib

import (
	"sort"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

type SortHost struct{}

func (h *SortHost) Ints(x *ffigo.ArrayRef[int64]) {
	if x == nil {
		return
	}
	sort.Slice(x.Value, func(i, j int) bool { return x.Value[i] < x.Value[j] })
}

func (h *SortHost) Float64s(x *ffigo.ArrayRef[float64]) {
	if x == nil {
		return
	}
	sort.Float64s(x.Value)
}

func (h *SortHost) Strings(x *ffigo.ArrayRef[string]) {
	if x == nil {
		return
	}
	sort.Strings(x.Value)
}

func (h *SortHost) IntsAreSorted(x []int64) bool {
	return sort.SliceIsSorted(x, func(i, j int) bool { return x[i] < x[j] })
}

func (h *SortHost) Float64sAreSorted(x []float64) bool { return sort.Float64sAreSorted(x) }
func (h *SortHost) StringsAreSorted(x []string) bool   { return sort.StringsAreSorted(x) }
