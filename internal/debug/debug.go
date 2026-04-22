package debug

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// Logger emits timestamped debug lines to stderr.
// A nil *Logger is safe to use — all methods are no-ops.
type Logger struct {
	mu    sync.Mutex
	start time.Time
}

// New creates a new debug logger anchored at now.
func New() *Logger {
	return &Logger{start: time.Now()}
}

// Enabled reports whether debug logging is active.
func (l *Logger) Enabled() bool {
	return l != nil
}

// Log writes a timestamped debug line to stderr.
// No-op when l is nil.
func (l *Logger) Log(format string, args ...any) {
	if l == nil {
		return
	}
	now := time.Now()
	elapsed := now.Sub(l.start)

	msg := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("[DEBUG %s +%s] %s\n",
		now.Format("15:04:05.000"),
		formatElapsed(elapsed),
		msg,
	)

	l.mu.Lock()
	fmt.Fprint(os.Stderr, line)
	l.mu.Unlock()
}

func formatElapsed(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.3fs", d.Seconds())
}
