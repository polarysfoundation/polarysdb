package logger

import (
	"io"
	"log"
	"os"
	"sync"
)

type Level int

const (
	LevelInfo Level = iota
	LevelWarn
	LevelError
	LevelFatal
	LevelDebug
)

type Logger struct {
	mu          sync.Once
	infoLogger  *log.Logger
	warnLogger  *log.Logger
	errorLogger *log.Logger
	fatalLogger *log.Logger
	debugLogger *log.Logger
	file        *os.File
	minLevel    Level
	toConsole   bool
	toFile      bool
}

// Config defines logger configuration options.
type Config struct {
	LogFilePath string // Path to the log file (optional)
	MinLevel    Level  // Minimum level to log
	ToConsole   bool   // Whether to log to console
	ToFile      bool   // Whether to log to file
}

// NewLogger creates a new logger instance with given configuration.
func NewLogger(cfg Config) *Logger {
	l := &Logger{
		minLevel:  cfg.MinLevel,
		toConsole: cfg.ToConsole,
		toFile:    cfg.ToFile,
	}

	l.mu.Do(func() {
		var file io.Writer
		if l.toFile && cfg.LogFilePath != "" {
			f, err := os.OpenFile(cfg.LogFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
			if err != nil {
				log.Fatalf("failed to open log file: %v", err)
			}
			l.file = f
			file = f
		}

		// Configure multi-output writers
		infoOut := l.buildWriter(os.Stdout, file)
		errorOut := l.buildWriter(os.Stderr, file)

		// Create level-based loggers
		l.infoLogger = log.New(infoOut, "\033[34mINFO:\033[0m ", log.Ldate|log.Ltime)
		l.warnLogger = log.New(infoOut, "\033[33mWARN:\033[0m ", log.Ldate|log.Ltime)
		l.errorLogger = log.New(errorOut, "\033[31mERROR:\033[0m ", log.Ldate|log.Ltime)
		l.fatalLogger = log.New(errorOut, "\033[35mFATAL:\033[0m ", log.Ldate|log.Ltime)
		l.debugLogger = log.New(infoOut, "\033[36mDEBUG:\033[0m ", log.Ldate|log.Ltime)
	})

	return l
}

// buildWriter creates an io.Writer to console, file or both.
func (l *Logger) buildWriter(console io.Writer, file io.Writer) io.Writer {
	switch {
	case l.toConsole && l.toFile:
		return io.MultiWriter(console, file)
	case l.toFile:
		return file
	case l.toConsole:
		return console
	default:
		return io.Discard
	}
}

// Info logs an informational message.
func (l *Logger) Info(v ...interface{}) {
	if l.minLevel > LevelInfo {
		return
	}
	l.infoLogger.Println(v...)
}

// Infof logs an informational message with formatting.
func (l *Logger) Infof(format string, v ...interface{}) {
	if l.minLevel > LevelInfo {
		return
	}
	l.infoLogger.Printf(format, v...)
}

// Warnf logs a warning message with formatting.
func (l *Logger) Warnf(format string, v ...interface{}) {
	if l.minLevel > LevelWarn {
		return
	}
	l.warnLogger.Printf(format, v...)
}

// Errorf logs an error message with formatting.
func (l *Logger) Errorf(format string, v ...interface{}) {
	if l.minLevel > LevelError {
		return
	}
	l.errorLogger.Printf(format, v...)
}

// Fatalf logs a fatal error with formatting and exits.
func (l *Logger) Fatalf(format string, v ...interface{}) {
	if l.minLevel > LevelFatal {
		return
	}
	l.fatalLogger.Fatalf(format, v...)
}

// Warn logs a warning message.
func (l *Logger) Warn(v ...interface{}) {
	if l.minLevel > LevelWarn {
		return
	}
	l.warnLogger.Println(v...)
}

// Error logs an error message.
func (l *Logger) Error(v ...interface{}) {
	if l.minLevel > LevelError {
		return
	}
	l.errorLogger.Println(v...)
}

// Fatal logs a fatal error and exits.
func (l *Logger) Fatal(v ...interface{}) {
	if l.minLevel > LevelFatal {
		return
	}
	l.fatalLogger.Fatalln(v...)
}

// Close closes the log file, if used.
func (l *Logger) Close() error {
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}
