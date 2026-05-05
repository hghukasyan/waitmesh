//go:build crashinject

package engine

import (
	"os"
	"sync"
)

// Optional crash injection for process-level reliability tests.
// Build with -tags crashinject and set WAITMESH_CRASH_POINT.
var (
	crashPoint     string
	crashPointOnce sync.Once
)

func initCrashPoint() {
	crashPointOnce.Do(func() {
		crashPoint = os.Getenv("WAITMESH_CRASH_POINT")
	})
}

func maybeCrash(point string) {
	initCrashPoint()
	if crashPoint != "" && crashPoint == point {
		os.Exit(90)
	}
}
