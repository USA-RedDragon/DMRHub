package logging

import (
	"fmt"
	"io"
	"log"
	"os"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

type LogType string

const (
	Access          LogType = LogType("access")
	Error           LogType = LogType("error")
	maxInFlightLogs         = 200
)

var (
	accessLog *Logger //nolint:golint,gochecknoglobals
	errorLog  *Logger //nolint:golint,gochecknoglobals
	isInit    atomic.Bool
	loaded    atomic.Bool
)

func getLogger(logType LogType) *Logger {
	switch logType {
	case Access:
		if accessLog != nil {
			return accessLog
		}
		// Create access logger
		return createLogger(logType)
	case Error:
		if errorLog != nil {
			return errorLog
		}
		// Create error logger
		return createLogger(logType)
	default:
		panic("Logging failed")
	}
}

func GetLogger(logType LogType) *Logger {
	lastInit := isInit.Swap(true)
	if !lastInit {
		logger := getLogger(logType)
		loaded.Store(true)
		return logger
	}
	for !loaded.Load() {
		const loadDelay = 100 * time.Nanosecond
		time.Sleep(loadDelay)
	}

	return getLogger(logType)
}

func createLogger(logType LogType) *Logger {
	var logFile *os.File
	switch runtime.GOOS {
	case "windows":
		logFile = createLocalLog(logType)
	case "darwin":
		logFile = createLocalLog(logType)
	default:
		// Check if /var/log/DMRHub exists
		// If not, create it. If we don't have permission
		// to create it, then create a local log file
		file := fmt.Sprintf("/var/log/DMRHub/DMRHub.%s.log", logType)
		if _, err := os.Stat("/var/log/DMRHub"); os.IsNotExist(err) {
			err := os.Mkdir("/var/log/DMRHub", 0755) //nolint:golint,gomnd
			if err != nil {
				logFile = createLocalLog(logType)
				break
			}
			err = os.Chown("/var/log/DMRHub", os.Getuid(), os.Getgid())
			if err != nil {
				logFile = createLocalLog(logType)
				break
			}

			logFile, err = os.OpenFile(file, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0665) //nolint:golint,gomnd
			if err != nil {
				logFile = createLocalLog(logType)
				break
			}
		} else {
			logFile, err = os.OpenFile(file, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0665) //nolint:golint,gomnd
			if err != nil {
				logFile = createLocalLog(logType)
				break
			}
		}
	}

	var sysLogger *log.Logger
	switch logType {
	case Access:
		sysLogger = log.New(logFile, "", log.LstdFlags)
	case Error:
		sysLogger = log.New(io.MultiWriter(os.Stderr, logFile), "", log.LstdFlags)
	}

	logger := &Logger{
		logger:  sysLogger,
		file:    logFile,
		Writer:  sysLogger.Writer(),
		channel: make(chan string, maxInFlightLogs),
	}

	go logger.Relay()

	return logger
}

func (l *Logger) Relay() {
	for msg := range l.channel {
		if msg != "" {
			l.logger.Print(msg)
		}
	}
}

func createLocalLog(logType LogType) *os.File {
	file := fmt.Sprintf("DMRHub.%s.log", logType)
	logFile, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0665) //nolint:golint,gomnd
	if err != nil {
		log.Fatalf("Failed to create log file: %s:\n%v", file, err)
	}
	return logFile
}

type Logger struct {
	logger  *log.Logger
	file    *os.File
	Writer  io.Writer
	channel chan string
}

// Pass the function itself to the logger
func (l *Logger) Log(function interface{}, format string) {
	l.channel <- fmt.Sprintf("%s: %s", getFunctionName(function), format)
}

func (l *Logger) Logf(function interface{}, format string, args ...interface{}) {
	l.channel <- fmt.Sprintf("%s: %s", getFunctionName(function), fmt.Sprintf(format, args...))
}

// Use a tiny bit of reflection to get the name of the function
func getFunctionName(i interface{}) string {
	name := runtime.FuncForPC(reflect.ValueOf(i).Pointer()).Name()
	return strings.TrimPrefix(name, "github.com/USA-RedDragon/DMRHub/")
}

func Close() {
	close(accessLog.channel)
	close(errorLog.channel)
	_ = accessLog.file.Close()
	_ = errorLog.file.Close()
}
