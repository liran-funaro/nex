package nex

import (
	"fmt"
)

func mustFunc(f func() bool, err error) {
	if f() {
		logger.Fatalf("nex: %v", err)
	}
}

func mustf(cond bool, format string, a ...any) {
	if !cond {
		logger.Fatalf("nex: %s", fmt.Sprintf(format, a...))
	}
}

func noError(err error, s string) {
	if err != nil {
		logger.Fatalf("nex: %s: %v", s, err)
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
