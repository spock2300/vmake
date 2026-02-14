package log

import (
	"fmt"
	"os"
)

type Level int

const (
	Quiet Level = iota
	Normal
	Verbose
	VeryVerbose
)

var level Level = Normal

func SetLevel(l Level) {
	level = l
}

func GetLevel() Level {
	return level
}

func Debug(format string, args ...any) {
	if level >= VeryVerbose {
		fmt.Printf(format+"\n", args...)
	}
}

func Info(format string, args ...any) {
	if level >= Normal {
		fmt.Printf(format+"\n", args...)
	}
}

func InfoNormal(format string, args ...any) {
	if level == Normal || level == Verbose {
		fmt.Printf(format+"\n", args...)
	}
}

func Error(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}
