package main

import (
	"fmt"
	"os"
)

func main() {
	roots := os.Args[1:]
	if len(roots) == 0 {
		roots = []string{"."}
	}
	violations, err := Scan(roots)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	for _, v := range violations {
		fmt.Println(v)
	}
	if len(violations) > 0 {
		os.Exit(1)
	}
}
