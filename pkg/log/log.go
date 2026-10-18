package log

import (
	"fmt"
	"os"
)

type Level int

const (
	Quiet Level = iota
	Normal
	VerboseLevel
	VeryVerbose
)

var level Level = Normal

func SetLevel(l Level) {
	level = l
}

// IsQuiet reports whether output is suppressed (-q).
func IsQuiet() bool {
	return level <= Quiet
}

func Debug(format string, args ...any) {
	if level >= VeryVerbose {
		fmt.Printf(format+"\n", args...)
	}
}

// Verbose prints detail-only output (-v) that is not part of the default
// progress view.
func Verbose(format string, args ...any) {
	if level >= VerboseLevel {
		fmt.Printf(format+"\n", args...)
	}
}

func Info(format string, args ...any) {
	if level >= Normal {
		fmt.Printf(format+"\n", args...)
	}
}

func Error(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

func Fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
