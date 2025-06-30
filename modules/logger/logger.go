package logger

import (
	"log"
	"os"
	"sync"
)

var (
	infoLogger  *log.Logger
	warnLogger  *log.Logger
	errorLogger *log.Logger
	fatalLogger *log.Logger
	logOnce     sync.Once
)

// Init sets up the loggers for different levels.
func Init() {
	logOnce.Do(func() {
		infoLogger = log.New(os.Stdout, "INFO: ", log.Ldate|log.Ltime|log.Lshortfile)
		warnLogger = log.New(os.Stdout, "WARN: ", log.Ldate|log.Ltime|log.Lshortfile)
		errorLogger = log.New(os.Stderr, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile)
		fatalLogger = log.New(os.Stderr, "FATAL: ", log.Ldate|log.Ltime|log.Lshortfile)
	})
}

// Info logs an informational message.
func Info(v ...interface{}) {
	Init() // Ensure loggers are initialized
	infoLogger.Println(v...)
}

// Warn logs a warning message.
func Warn(v ...interface{}) {
	Init() // Ensure loggers are initialized
	warnLogger.Println(v...)
}

// Error logs an error message.
func Error(v ...interface{}) {
	Init() // Ensure loggers are initialized
	errorLogger.Println(v...)
}

// Fatal logs a fatal message and exits the program.
func Fatal(v ...interface{}) {
	Init() // Ensure loggers are initialized
	fatalLogger.Fatalln(v...)
}
