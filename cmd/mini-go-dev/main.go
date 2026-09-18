package main

import (
	"fmt"
	"os"
)

func main() {
	if err := runDevCLI(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
