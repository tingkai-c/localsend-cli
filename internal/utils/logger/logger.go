// logger/logger.go
package logger

import (
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

type Logger struct {
	*logrus.Logger
}

// LogConfig 定义日志配置选项
type LogConfig struct {
	Level        logrus.Level
	Output       io.Writer
	Formatter    logrus.Formatter
	ReportCaller bool
}

// LogEvent is a UI-safe copy of a log entry. It deliberately contains only
// presentation-neutral data so callers can render it without importing logrus.
type LogEvent struct {
	Level   logrus.Level
	Message string
	Time    time.Time
}

var (
	logger *Logger
	once   sync.Once

	// ANSI 颜色代码
	green = "\033[32m"
	red   = "\033[31m"
	reset = "\033[0m"
)

// DefaultConfig 返回默认配置
func DefaultConfig() LogConfig {
	return LogConfig{
		Level:  logrus.InfoLevel,
		Output: os.Stdout,
		Formatter: &logrus.TextFormatter{
			FullTimestamp: true,
			ForceColors:   true,
		},
		ReportCaller: false,
	}
}

// InitLogger 初始化 Logger
func InitLogger(config ...LogConfig) {
	once.Do(func() {
		cfg := DefaultConfig()
		if len(config) > 0 {
			cfg = config[0]
		}

		log := logrus.New()
		log.SetOutput(cfg.Output)
		log.SetFormatter(cfg.Formatter)
		log.SetLevel(cfg.Level)
		log.SetReportCaller(cfg.ReportCaller)

		logger = &Logger{log}
	})
}

// checkLogger 确保 logger 已初始化
func checkLogger() {
	if logger == nil {
		InitLogger() // 使用默认配置初始化
	}
}

// GetLogger 返回底层 Logger 实例
func GetLogger() *Logger {
	checkLogger()
	return logger
}

// SuppressInfoAndBelow raises the active logger threshold while a caller owns a
// terminal UI that would be corrupted by background informational logs. The
// returned function restores the previous threshold and is safe to call more
// than once.
func SuppressInfoAndBelow() func() {
	checkLogger()

	previousLevel := logger.GetLevel()
	if previousLevel > logrus.WarnLevel {
		logger.SetLevel(logrus.WarnLevel)
	}

	var restoreOnce sync.Once
	return func() {
		restoreOnce.Do(func() {
			logger.SetLevel(previousLevel)
		})
	}
}

// CaptureWarningsAndErrors routes warning-and-above log entries into a
// non-blocking event channel while preventing the logger from writing directly
// to the terminal. It is intended for terminal UIs that need to render log
// feedback inside the model instead of letting background logs corrupt the
// screen. The returned restore function is idempotent.
func CaptureWarningsAndErrors(buffer int) (<-chan LogEvent, func()) {
	checkLogger()
	if buffer < 1 {
		buffer = 1
	}

	events := make(chan LogEvent, buffer)
	previousLevel := logger.GetLevel()
	previousOutput := logger.Out

	active := &atomic.Bool{}
	active.Store(true)
	logger.AddHook(&logEventHook{events: events, active: active})
	if previousLevel > logrus.WarnLevel {
		logger.SetLevel(logrus.WarnLevel)
	}
	logger.SetOutput(io.Discard)

	var restoreOnce sync.Once
	return events, func() {
		restoreOnce.Do(func() {
			active.Store(false)
			logger.SetOutput(previousOutput)
			logger.SetLevel(previousLevel)
		})
	}
}

type logEventHook struct {
	events chan<- LogEvent
	active *atomic.Bool
}

func (h *logEventHook) Levels() []logrus.Level {
	return []logrus.Level{
		logrus.PanicLevel,
		logrus.FatalLevel,
		logrus.ErrorLevel,
		logrus.WarnLevel,
	}
}

func (h *logEventHook) Fire(entry *logrus.Entry) error {
	if h == nil || h.active == nil || !h.active.Load() {
		return nil
	}
	event := LogEvent{
		Level:   entry.Level,
		Message: entry.Message,
		Time:    entry.Time,
	}
	select {
	case h.events <- event:
	default:
	}
	return nil
}

// Success 打印带有绿色 [Success] 标签的信息
func Success(args ...interface{}) {
	checkLogger()
	logger.Infof("%s[Success]%s %s", green, reset, fmt.Sprint(args...))
}

// Successf 打印带有绿色 [Success] 标签的格式化信息
func Successf(format string, args ...interface{}) {
	checkLogger()
	logger.Infof("%s[Success]%s %s", green, reset, fmt.Sprintf(format, args...))
}

// Failed 打印带有红色 [Failed] 标签的信息
func Failed(args ...interface{}) {
	checkLogger()
	logger.Errorf("%s[Failed]%s %s", red, reset, fmt.Sprint(args...))
}

// Failedf 打印带有红色 [Failed] 标签的格式化信息
func Failedf(format string, args ...interface{}) {
	checkLogger()
	logger.Errorf("%s[Failed]%s %s", red, reset, fmt.Sprintf(format, args...))
}

func Debug(args ...interface{}) {
	checkLogger()
	logger.Debug(args...)
}

func Debugf(format string, args ...interface{}) {
	checkLogger()
	logger.Debugf(format, args...)
}

// Info 打印信息级别日志
func Info(args ...interface{}) {
	checkLogger()
	logger.Info(args...)
}

// Infof 打印信息级别日志（支持格式化）
func Infof(format string, args ...interface{}) {
	checkLogger()
	logger.Infof(format, args...)
}

// Warn 打印警告级别日志
func Warn(args ...interface{}) {
	checkLogger()
	logger.Warn(args...)
}

// Warnf 打印警告级别日志（支持格式化）
func Warnf(format string, args ...interface{}) {
	checkLogger()
	logger.Warnf(format, args...)
}

// Error 打印错误级别日志
func Error(args ...interface{}) {
	checkLogger()
	logger.Error(args...)
}

// Errorf 打印错误级别日志（支持格式化）
func Errorf(format string, args ...interface{}) {
	checkLogger()
	logger.Errorf(format, args...)
}

// WithFields 支持结构化日志
func WithFields(fields logrus.Fields) *Logger {
	checkLogger()
	return &Logger{logger.WithFields(fields).Logger}
}
