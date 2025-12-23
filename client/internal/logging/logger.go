package logging

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Level задаёт минимальный уровень сообщений, которые попадут в лог.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelError
)

var levelNames = map[string]Level{
	"debug": LevelDebug,
	"info":  LevelInfo,
	"error": LevelError,
}

// ParseLevel преобразует строковое значение из конфигурации в Level.
func ParseLevel(value string) Level {
	value = strings.TrimSpace(strings.ToLower(value))
	if lvl, ok := levelNames[value]; ok {
		return lvl
	}
	return LevelInfo
}

// Logger представляет потокобезопасный файловый логгер с уровнями.
type Logger struct {
	minLevel Level
	writer   io.Writer
	closer   io.Closer
	mu       sync.Mutex
}

// New создаёт новый логгер, пишущий в указанный файл.
func New(path string, level Level) (*Logger, error) {
	if path == "" {
		return nil, fmt.Errorf("log path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create log directory for %s: %w", path, err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log file %s: %w", path, err)
	}
	return &Logger{minLevel: level, writer: file, closer: file}, nil
}

// Close освобождает ресурсы файлового логгера.
func (l *Logger) Close() error {
	if l == nil || l.closer == nil {
		return nil
	}
	return l.closer.Close()
}

// Debugf пишет отладочное сообщение.
func (l *Logger) Debugf(format string, args ...any) {
	l.write(LevelDebug, format, args...)
}

// Infof пишет информационное сообщение.
func (l *Logger) Infof(format string, args ...any) {
	l.write(LevelInfo, format, args...)
}

// Errorf пишет сообщение об ошибке.
func (l *Logger) Errorf(format string, args ...any) {
	l.write(LevelError, format, args...)
}

func (l *Logger) write(level Level, format string, args ...any) {
	if l == nil || level < l.minLevel {
		return
	}
	entry := fmt.Sprintf(format, args...)
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.writer, "%s [%s] %s\n", time.Now().UTC().Format(time.RFC3339), level.String(), entry)
}

// Level возвращает минимальный уровень логгера.
func (l *Logger) Level() Level {
	if l == nil {
		return LevelInfo
	}
	return l.minLevel
}

// String возвращает текстовое представление уровня.
func (lvl Level) String() string {
	switch lvl {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// ProcessLogPath формирует путь к лог-файлу дочернего процесса (Core).
func ProcessLogPath(appDir string, processName string) string {
	logsDir := filepath.Join(appDir, "logs")
	filename := fmt.Sprintf("%s.log", strings.ToLower(processName))
	return filepath.Join(logsDir, filename)
}

var loggerContextKey struct{}

// WithContext сохраняет логгер в контексте для дальнейшей передачи.
func WithContext(ctx context.Context, logger *Logger) context.Context {
	return context.WithValue(ctx, loggerContextKey, logger)
}

// FromContext извлекает логгер из контекста, если он там есть.
func FromContext(ctx context.Context) (*Logger, bool) {
	logger, ok := ctx.Value(loggerContextKey).(*Logger)
	return logger, ok
}
