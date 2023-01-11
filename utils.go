package main

import (
	"fmt"
	"os"
)

func log(log string, a ...interface{}) {
	if *verbose {
		fmt.Printf(log+"\n", a...)
	}
}

func handleError(log string, err error) {
	if err != nil {
		fmt.Println(log, err)
		os.Exit(1)
	}
}
