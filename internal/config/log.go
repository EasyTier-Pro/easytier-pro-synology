package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	logMaxBytes = 5 * 1024 * 1024
	logKeep     = 1
)

// Logger appends timestamped lines to the daemon log with size-based rotation
// and mirrors them to stderr, which DSM captures for the package log.
type Logger struct {
	path    string
	verbose bool

	mu   sync.Mutex
	file *os.File
	size int64
}

// NewLogger opens path for appending and probes the current size.
func NewLogger(path string, verbose bool) (*Logger, error) {
	logger := &Logger{path: path, verbose: verbose}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if info, err := os.Stat(path); err == nil {
		logger.size = info.Size()
	}
	return logger, nil
}

// Printf writes one log line.
func (l *Logger) Printf(format string, args ...any) {
	l.log("info", fmt.Sprintf(format, args...))
}

// Errorf writes one error line.
func (l *Logger) Errorf(format string, args ...any) {
	l.log("error", fmt.Sprintf(format, args...))
}

func (l *Logger) log(level, message string) {
	line := fmt.Sprintf("%s %s %s\n", time.Now().Format("2006-01-02 15:04:05"), level, message)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		l.open()
	}
	if l.file != nil {
		n, err := l.file.WriteString(line)
		if err == nil {
			l.size += int64(n)
			if l.size > logMaxBytes {
				l.rotate()
			}
		}
	}
	if l.verbose {
		os.Stderr.WriteString(line)
	}
}

func (l *Logger) open() {
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	l.file = file
}

func (l *Logger) rotate() {
	if l.file != nil {
		l.file.Close()
		l.file = nil
	}
	for i := logKeep; i > 1; i-- {
		os.Rename(rotatedPath(l.path, i-1), rotatedPath(l.path, i))
	}
	os.Rename(l.path, rotatedPath(l.path, 1))
	l.size = 0
	l.open()
}

func rotatedPath(path string, index int) string {
	return fmt.Sprintf("%s.%d", path, index)
}

// Close releases the log file.
func (l *Logger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		l.file.Close()
		l.file = nil
	}
}
