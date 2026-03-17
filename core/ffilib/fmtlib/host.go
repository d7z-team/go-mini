package fmtlib

import (
	"fmt"
	"io"
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

func (h *FmtHost) Fprint(w any, args ...any) {
	if writer, ok := w.(io.Writer); ok {
		fmt.Fprint(writer, args...)
	}
}

func (h *FmtHost) Fprintf(w any, format string, args ...any) {
	if writer, ok := w.(io.Writer); ok {
		fmt.Fprintf(writer, format, args...)
	}
}

func (h *FmtHost) Fprintln(w any, args ...any) {
	if writer, ok := w.(io.Writer); ok {
		fmt.Fprintln(writer, args...)
	}
}
