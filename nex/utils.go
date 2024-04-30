package nex

import (
	"fmt"
	"log"
)

func mustFunc(f func() bool, err error) {
	if f() {
		log.Fatalf("nex: %v", err)
	}
}

func Mustf(cond bool, format string, a ...any) {
	if !cond {
		log.Fatalf("nex: %s", fmt.Sprintf(format, a...))
	}
}

func NoError(err error, s string) {
	if err != nil {
		log.Fatalf("nex: %s: %v", s, err)
	}
}

func findNthLineIndex(buffer string, n int) int {
	if n <= 0 {
		return 0
	}
	for i, b := range buffer {
		if b == '\n' {
			n--
			if n <= 0 {
				return i + 1
			}
		}
	}
	return len(buffer)
}
