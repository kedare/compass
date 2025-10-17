// Package logger provides structured logging configuration for the compass application
package logger

import (
	"os"
	"strings"

	"github.com/sirupsen/logrus"
)

var Log *logrus.Logger

func init() {
	Log = logrus.New()
	Log.SetOutput(os.Stdout)
	Log.SetFormatter(&logrus.TextFormatter{
		DisableColors:    false,
		DisableTimestamp: false,
		FullTimestamp:    true,
		TimestampFormat:  "15:04:05",
	})
	Log.SetLevel(logrus.InfoLevel)
}

func SetLevel(level string) error {
	logLevel, err := logrus.ParseLevel(strings.ToLower(level))
	if err != nil {
		return err
	}

	Log.SetLevel(logLevel)

	return nil
}

func GetLogger() *logrus.Logger {
	return Log
}
