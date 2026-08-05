package logger

import (
	"sync"
	"testing"
)

// TestConcurrentLoggersDoNotRace is a regression test: New() used to set the
// fatih/color package's global NoColor variable as a side effect of
// construction. That's harmless with a single Logger, but under `go test
// -race` two Loggers with different colorize settings created and used
// concurrently (exactly what happens across this repo's other test
// packages, each building their own test Logger) reliably raced on that
// global. Run with `go test -race` to verify.
func TestConcurrentLoggersDoNotRace(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		colorize := i%2 == 0
		go func() {
			defer wg.Done()
			l := New("[test]", false, colorize)
			l.Infof("hello %d", i)
			l.Warnf("warn %d", i)
			l.Errorf("err %d", i)
			l.Successf("ok %d", i)
			l.ExtraLine("extra", "line")
			l.BuildError("boom")
		}()
	}
	wg.Wait()
}
