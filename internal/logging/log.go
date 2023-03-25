// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023 Jacob McSwain
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.
//
// The source code is available at <https://github.com/USA-RedDragon/DMRHub>
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
	accessLog    *Logger     //nolint:golint,gochecknoglobals
	errorLog     *Logger     //nolint:golint,gochecknoglobals
	isAccessInit atomic.Bool //nolint:golint,gochecknoglobals
	accessLoaded atomic.Bool //nolint:golint,gochecknoglobals
	isErrorInit  atomic.Bool //nolint:golint,gochecknoglobals
	errorLoaded  atomic.Bool //nolint:golint,gochecknoglobals
)

func GetLogger(logType LogType) *Logger {
	const loadDelay = 100 * time.Nanosecond

	switch logType {
	case Access:
		lastInit := isAccessInit.Swap(true)
		if !lastInit {
			accessLog = createLogger(logType)
			accessLoaded.Store(true)
		}
		for !accessLoaded.Load() {
			time.Sleep(loadDelay)
		}
		return accessLog
	case Error:
		lastInit := isErrorInit.Swap(true)
		if !lastInit {
			errorLog = createLogger(logType)
			errorLoaded.Store(true)
		}
		for !errorLoaded.Load() {
			time.Sleep(loadDelay)
		}
		return errorLog
	default:
		panic("Logging failed")
	}
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
