package sortlib

import (
	"sort"
)

type SortHost struct{}

func (h *SortHost) Ints(x []int64) []int64 {
	if len(x) == 0 {
		return x
	}
	sort.Slice(x, func(i, j int) bool { return x[i] < x[j] })
	return x
}

func (h *SortHost) Float64s(x []float64) []float64 {
	sort.Float64s(x)
	return x
}

func (h *SortHost) Strings(x []string) []string {
	sort.Strings(x)
	return x
}

func (h *SortHost) IntsAreSorted(x []int64) bool {
	return sort.SliceIsSorted(x, func(i, j int) bool { return x[i] < x[j] })
}

func (h *SortHost) Float64sAreSorted(x []float64) bool { return sort.Float64sAreSorted(x) }
func (h *SortHost) StringsAreSorted(x []string) bool   { return sort.StringsAreSorted(x) }
