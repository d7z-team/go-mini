package fmtlib

import (
	"fmt"
)

type FmtHost struct{}

func (h *FmtHost) Print(args ...any) {
	fmt.Print(args...)
}

func (h *FmtHost) Println(args ...any) {
	fmt.Println(args...)
}

func (h *FmtHost) Printf(format string, args ...any) {
	fmt.Printf(format, args...)
}

func (h *FmtHost) Sprintf(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}
