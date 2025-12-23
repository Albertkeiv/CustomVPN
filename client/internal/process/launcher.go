package process

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"customvpn/client/internal/logging"
	"customvpn/client/internal/state"
)

type handle struct {
	cmd    *exec.Cmd
	exitCh chan struct{}
}

// ExitCallback вызывается при завершении процесса.
type ExitCallback func(name state.ProcessName, exitCode int, reason string)

// Launcher отвечает за запуск и остановку процессов Core.
type Launcher struct {
	logger *logging.Logger
	mu     sync.Mutex
	procs  map[state.ProcessName]*handle
	onExit ExitCallback
}

// NewLauncher создаёт новый Launcher.
func NewLauncher(logger *logging.Logger) *Launcher {
	return &Launcher{logger: logger, procs: make(map[state.ProcessName]*handle)}
}

// SetExitCallback задаёт функцию, вызываемую при завершении процессов.
func (l *Launcher) SetExitCallback(cb ExitCallback) {
	l.mu.Lock()
	l.onExit = cb
	l.mu.Unlock()
}

// Start запускает процесс с заданными аргументами и перенаправлением вывода в файл.
func (l *Launcher) Start(name state.ProcessName, binary string, args []string, logFile string) (*state.ProcessRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if binary == "" {
		return nil, fmt.Errorf("binary path is empty")
	}
	if _, exists := l.procs[name]; exists {
		return nil, fmt.Errorf("process %s already running", name)
	}
	cmd := exec.Command(binary, args...)
	cmd.Dir = filepath.Dir(binary)
	applyProcessAttributes(cmd)
	if l.logger != nil {
		l.logger.Debugf("launch %s: %s", name, formatCommand(binary, args))
	}
	logWriter, err := openLogFile(logFile)
	if err != nil {
		return nil, err
	}
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter
	if err := cmd.Start(); err != nil {
		logWriter.Close()
		return nil, fmt.Errorf("start %s: %w", name, err)
	}
	h := &handle{cmd: cmd, exitCh: make(chan struct{})}
	l.procs[name] = h
	record := &state.ProcessRecord{
		Name:      name,
		Command:   binary,
		Args:      append([]string{}, args...),
		PID:       cmd.Process.Pid,
		StartedAt: time.Now(),
		Status:    state.ProcessRunning,
	}
	go func() {
		err := cmd.Wait()
		logWriter.Close()
		l.finishProcess(name, h, err)
	}()
	return record, nil
}

func formatCommand(binary string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, quoteArg(binary))
	for _, arg := range args {
		parts = append(parts, quoteArg(arg))
	}
	return strings.Join(parts, " ")
}

func quoteArg(arg string) string {
	if arg == "" {
		return "\"\""
	}
	if strings.IndexAny(arg, " \t\"") == -1 {
		return arg
	}
	escaped := strings.ReplaceAll(arg, "\"", "\\\"")
	return "\"" + escaped + "\""
}

// Stop пытается корректно завершить процесс, затем применяет kill по таймауту.
func (l *Launcher) Stop(name state.ProcessName, timeout time.Duration) error {
	l.mu.Lock()
	h := l.procs[name]
	l.mu.Unlock()
	if h == nil || h.cmd == nil || h.cmd.Process == nil {
		return nil
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if err := sendInterrupt(h.cmd); err != nil {
		if l.logger != nil {
			l.logger.Debugf("send interrupt to %s failed: %v", name, err)
		}
	}
	select {
	case <-h.exitCh:
		return nil
	case <-time.After(timeout):
		l.logger.Infof("process %s timeout, killing", name)
		if err := h.cmd.Process.Kill(); err != nil {
			return err
		}
		<-h.exitCh
		return nil
	}
}

func openLogFile(path string) (io.WriteCloser, error) {
	if path == "" {
		return nil, fmt.Errorf("log file path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
}

func (l *Launcher) finishProcess(name state.ProcessName, h *handle, err error) {
	l.mu.Lock()
	if current, ok := l.procs[name]; ok && current == h {
		delete(l.procs, name)
	}
	close(h.exitCh)
	cb := l.onExit
	l.mu.Unlock()
	exitCode := 0
	reason := "process exited normally"
	if err != nil {
		reason = err.Error()
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
		l.logger.Errorf("process %s exited with error: %s", name, reason)
	} else {
		l.logger.Infof("process %s exited", name)
	}
	if cb != nil {
		cb(name, exitCode, reason)
	}
}
