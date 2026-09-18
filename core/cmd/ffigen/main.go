package main

import (
	"flag"
	"fmt"
	"os"

	"gopkg.d7z.net/go-mini/core/ffigen"
)

func main() {
	pkgName := flag.String("pkg", "", "package name")
	outFile := flag.String("out", "", "output file")
	flag.Parse()

	if err := ffigen.Run(ffigen.Options{
		PackageName: *pkgName,
		Output:      *outFile,
		Args:        flag.Args(),
	}); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
