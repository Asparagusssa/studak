// Package logx contains logging helpers for errors at process and request
// boundaries. Client responses must never include these stack traces.
package logx

import (
	"log"
	"runtime/debug"
)

// Error logs an error together with the current goroutine stack. Call it only
// when an error is handled for the last time; logging at every return point
// would duplicate the same failure many times.
func Error(operation string, err error) {
	if err == nil {
		return
	}
	log.Printf("ERROR %s: %v\nstacktrace:\n%s", operation, err, debug.Stack())
}
