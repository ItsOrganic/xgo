package logger

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/fatih/color"
)

// Logger is a leveled logger with colored, prefixed lines.
type Logger struct {
	mu         sync.Mutex
	out        io.Writer
	err        io.Writer
	prefix     string
	timestamps bool
	colorize   bool
	extraColor map[string]*color.Color
	colorIdx   int
}

var palette = []*color.Color{
	color.New(color.FgHiMagenta),
	color.New(color.FgHiBlue),
	color.New(color.FgHiYellow),
	color.New(color.FgHiCyan),
	color.New(color.FgHiWhite),
	color.New(color.FgHiGreen),
}

// New creates a logger.
func New(prefix string, timestamps, colorize bool) *Logger {
	color.NoColor = !colorize
	if prefix == "" {
		prefix = "[xgo]"
	}
	return &Logger{
		out:        os.Stdout,
		err:        os.Stderr,
		prefix:     prefix,
		timestamps: timestamps,
		colorize:   colorize,
		extraColor: make(map[string]*color.Color),
	}
}

// Infof prints an info line.
func (l *Logger) Infof(format string, args ...any) {
	l.printLevel(l.out, "INFO", color.FgWhite, format, args...)
}

// Warnf prints a warning line.
func (l *Logger) Warnf(format string, args ...any) {
	l.printLevel(l.out, "WARN", color.FgYellow, format, args...)
}

// Errorf prints an error line.
func (l *Logger) Errorf(format string, args ...any) {
	l.printLevel(l.err, "ERROR", color.FgRed, format, args...)
}

// Successf prints a success line.
func (l *Logger) Successf(format string, args ...any) {
	l.printLevel(l.out, "SUCCESS", color.FgGreen, format, args...)
}

// BuildError prints a prominent build error block.
func (l *Logger) BuildError(output string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	sep := color.New(color.FgRed, color.Bold).Sprint("================================ BUILD ERROR ================================")
	fmt.Fprintln(l.err, sep)
	fmt.Fprintln(l.err, color.New(color.FgRed).Sprint(output))
	fmt.Fprintln(l.err, sep)
}

// ExtraLine prints output for a named extra command.
func (l *Logger) ExtraLine(name, line string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	c, ok := l.extraColor[name]
	if !ok {
		c = palette[l.colorIdx%len(palette)]
		l.colorIdx++
		l.extraColor[name] = c
	}
	tstamp := ""
	if l.timestamps {
		tstamp = time.Now().Format("15:04:05") + " "
	}
	prefix := color.New(color.FgCyan, color.Bold).Sprint(l.prefix)
	namePrefix := c.Sprintf("[%s]", name)
	fmt.Fprintf(l.out, "%s%s %s %s\n", tstamp, prefix, namePrefix, line)
}

func (l *Logger) printLevel(w io.Writer, level string, fg color.Attribute, format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	msg := fmt.Sprintf(format, args...)
	tstamp := ""
	if l.timestamps {
		tstamp = time.Now().Format("15:04:05") + " "
	}
	prefix := color.New(color.FgCyan, color.Bold).Sprint(l.prefix)
	lvl := color.New(fg, color.Bold).Sprintf("[%s]", level)
	fmt.Fprintf(w, "%s%s %s %s\n", tstamp, prefix, lvl, msg)
}
