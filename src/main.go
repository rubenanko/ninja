package main

import (
	"fmt"
	"os"
)

func main() {
	e := NewEditor()
	if len(os.Args) > 1 {
		e.openFile(os.Args[1])
	}
	if err := e.run(); err != nil {
		fmt.Fprintln(os.Stderr, "tinyedit:", err)
		os.Exit(1)
	}
}
