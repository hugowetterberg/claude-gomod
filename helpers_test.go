package main

import (
	"fmt"
	"testing"
)

// mustf fails the test if err is non-nil, reporting a
// message built from format and args.
func mustf(tb testing.TB, err error, format string, a ...any) {
	tb.Helper()

	if err != nil {
		tb.Fatalf("failed: %s: %v", fmt.Sprintf(format, a...), err)
	}
}
