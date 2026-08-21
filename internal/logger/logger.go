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
	palette    []*color.Color
	extraColor map[string]*color.Color
	colorIdx   int
}

var paletteAttrs = []color.Attribute{
	color.FgHiMagenta,
	color.FgHiBlue,
	color.FgHiYellow,
	color.FgHiCyan,
	color.FgHiWhite,
	color.FgHiGreen,
}

// New creates a logger.
//
// Color state is kept entirely on the *color.Color instances this Logger
// owns (via EnableColor/DisableColor), never on the fatih/color package's
// global NoColor variable. Mutating that global used to make color
// behavior a process-wide side effect of construction - harmless with a
// single Logger, but a genuine data race the moment more than one exists
// concurrently (as any test creating multiple Loggers found immediately).
func New(prefix string, timestamps, colorize bool) *Logger {
	if prefix == "" {
		prefix = "[whack]"
	}
	palette := make([]*color.Color, len(paletteAttrs))
	for i, a := range paletteAttrs {
		palette[i] = newColor(colorize, a)
	}
	return &Logger{
		out:        os.Stdout,
		err:        os.Stderr,
		prefix:     prefix,
		timestamps: timestamps,
		colorize:   colorize,
		palette:    palette,
		extraColor: make(map[string]*color.Color),
	}
}

func newColor(colorize bool, attrs ...color.Attribute) *color.Color {
	c := color.New(attrs...)
	if colorize {
		c.EnableColor()
	} else {
		c.DisableColor()
	}
	return c
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
	sepColor := newColor(l.colorize, color.FgRed, color.Bold)
	bodyColor := newColor(l.colorize, color.FgRed)
	sep := sepColor.Sprint("================================ BUILD ERROR ================================")
	fmt.Fprintln(l.err, sep)
	fmt.Fprintln(l.err, bodyColor.Sprint(output))
	fmt.Fprintln(l.err, sep)
}

// ExtraLine prints output for a named extra command.
func (l *Logger) ExtraLine(name, line string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	c, ok := l.extraColor[name]
	if !ok {
		c = l.palette[l.colorIdx%len(l.palette)]
		l.colorIdx++
		l.extraColor[name] = c
	}
	tstamp := ""
	if l.timestamps {
		tstamp = time.Now().Format("15:04:05") + " "
	}
	prefix := newColor(l.colorize, color.FgCyan, color.Bold).Sprint(l.prefix)
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
	prefix := newColor(l.colorize, color.FgCyan, color.Bold).Sprint(l.prefix)
	lvl := newColor(l.colorize, fg, color.Bold).Sprintf("[%s]", level)
	fmt.Fprintf(w, "%s%s %s %s\n", tstamp, prefix, lvl, msg)
}
