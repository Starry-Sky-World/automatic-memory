package logging

import (
	"log"
	"os"
	"strings"
)

type Logger struct {
	level string
	base  *log.Logger
}

func New(level string) *Logger {
	lv := strings.ToLower(strings.TrimSpace(level))
	if lv == "" {
		lv = "info"
	}
	return &Logger{level: lv, base: log.New(os.Stdout, "", log.LstdFlags)}
}

func (l *Logger) enabled(level string) bool {
	order := map[string]int{"debug": 10, "info": 20, "warn": 30, "error": 40}
	cur, ok := order[l.level]
	if !ok {
		cur = 20
	}
	v, ok := order[level]
	if !ok {
		v = 20
	}
	return v >= cur
}

func (l *Logger) Debugf(format string, args ...any) {
	if l.enabled("debug") {
		l.base.Printf("[DEBUG] "+format, args...)
	}
}

func (l *Logger) Infof(format string, args ...any) {
	if l.enabled("info") {
		l.base.Printf("[INFO] "+format, args...)
	}
}

func (l *Logger) Warnf(format string, args ...any) {
	if l.enabled("warn") {
		l.base.Printf("[WARN] "+format, args...)
	}
}

func (l *Logger) Errorf(format string, args ...any) {
	if l.enabled("error") {
		l.base.Printf("[ERROR] "+format, args...)
	}
}
