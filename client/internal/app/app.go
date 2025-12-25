package app

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"customvpn/client/internal/config"
	"customvpn/client/internal/controlclient"
	"customvpn/client/internal/dns"
	"customvpn/client/internal/firewall"
	"customvpn/client/internal/logging"
	"customvpn/client/internal/process"
	"customvpn/client/internal/routes"
	"customvpn/client/internal/state"
	"customvpn/client/internal/ui"
)

// Application связывает state machine и контрольный сервер.
type Application struct {
	cfg        *config.Config
	logger     *logging.Logger
	control    *controlclient.Client
	machine    *state.Machine
	ctx        *state.AppContext
	routes     *routes.Manager
	firewall   *firewall.Manager
	dns        *dns.Manager
	launcher   *process.Launcher
	controlIP4 net.IP
	ui         *ui.Manager
	cleanupOnce sync.Once
	shutdown   chan struct{}
	runCtx     context.Context
	runCancel  context.CancelFunc
	stopOnce   sync.Once
}

// New создаёт Application и настраивает state machine callbacks.
func New(cfg *config.Config, logger *logging.Logger) (*Application, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger is nil")
	}
	client, err := controlclient.New(cfg.ControlServerURL, controlclient.Options{Logger: logger})
	if err != nil {
		return nil, fmt.Errorf("init control client: %w", err)
	}
	stateCtx := state.NewAppContext(cfg)
	runCtx, runCancel := context.WithCancel(context.Background())
	app := &Application{
		cfg:      cfg,
		logger:   logger,
		control:  client,
		ctx:      stateCtx,
		routes:   routes.NewManager(logger),
		firewall: firewall.NewManager(logger),
		dns:      dns.NewManager(logger),
		launcher: process.NewLauncher(logger),
		shutdown: make(chan struct{}),
		runCtx:   runCtx,
		runCancel: runCancel,
	}
	app.launcher.SetExitCallback(app.onProcessExit)
	uiManager := ui.NewManager(ui.Options{
		AppID:    "customvpn.client",
		AppName:  "CustomVPN",
		Logger:   logger,
		Dispatch: app.dispatch,
	})
	uiManager.SetOnStopped(app.onAppStopped)
	app.ui = uiManager
	callbacks := state.Callbacks{
		StartPreflight:      app.startPreflight,
		StartAuth:           app.startAuth,
		StartSync:           app.startSync,
		StartPrepareEnv:     app.startPrepareEnv,
		StartConnecting:     app.startConnecting,
		StartDisconnecting:  app.startDisconnecting,
		ForceCleanup:        app.forceCleanup,
		CleanupAndExit:      app.cleanupAndExit,
		ShowLoginWindow:     uiManager.ShowLoginWindow,
		ShowMainWindow:      uiManager.ShowMainWindow,
		HideMainWindow:      uiManager.HideMainWindow,
		UpdateUI:            uiManager.UpdateUI,
		ShowModalError:      uiManager.ShowModalError,
		ShowTransientNotice: uiManager.ShowTransientNotice,
		ShowCleanupStarted:  uiManager.ShowCleanupStarted,
		ShowCleanupDone:     uiManager.ShowCleanupDone,
	}
	app.machine = state.NewMachine(stateCtx, logger, callbacks)
	return app, nil
}

// Run запускает state machine и инициирует сценарий старта.
func (a *Application) Run() error {
	if a.machine == nil {
		return fmt.Errorf("machine is not initialized")
	}
	if a.ui != nil {
		a.ui.Start()
		a.ui.UpdateUI(a.ctx)
	}
	a.machine.Start()
	return a.dispatch(state.Event{Type: state.EventUILaunch, TS: time.Now()})
}

// RunUILoop запускает главный цикл Fyne и блокирует вызывающую горутину до выхода.
func (a *Application) RunUILoop() {
	if a.ui == nil {
		return
	}
	a.ui.RunMainLoop()
}

// Stop останавливает state machine.
func (a *Application) Stop() {
	a.stopOnce.Do(func() {
		if a.runCancel != nil {
			a.runCancel()
		}
		if a.launcher != nil {
			_ = a.launcher.Stop(state.ProcessCore, 2*time.Second)
		}
		if a.ui != nil {
			a.ui.Shutdown()
			if !a.ui.WaitAsync(3*time.Second) && a.logger != nil {
				a.logger.Errorf("ui background tasks did not finish before timeout")
			}
		}
		if a.machine != nil {
			a.machine.Stop()
			if !a.machine.WaitAsync(3 * time.Second) && a.logger != nil {
				a.logger.Errorf("state machine background tasks did not finish before timeout")
			}
		}
		close(a.shutdown)
	})
}

func (a *Application) dispatch(evt state.Event) error {
	if err := a.machine.Dispatch(evt); err != nil {
		a.logger.Errorf("dispatch %s failed: %v", evt.Type, err)
		return err
	}
	return nil
}

// Done возвращает канал, закрывающийся после полной остановки приложения.
func (a *Application) Done() <-chan struct{} {
	return a.shutdown
}

func (a *Application) cleanupAndExit(_ *state.AppContext) {
	a.logger.Infof("state machine requested shutdown")
	a.cleanupOnce.Do(func() { a.runExitCleanup() })
	if a.ui != nil {
		a.ui.Quit()
	}
	a.Stop()
}

func (a *Application) onProcessExit(name state.ProcessName, exitCode int, reason string) {
	if a.ctx == nil {
		return
	}
	record, ok := a.ctx.ProcessRegistry.Get(name)
	if !ok {
		record = state.ProcessRecord{Name: name}
	}
	status := state.ProcessExited
	if exitCode != 0 {
		status = state.ProcessFailed
	}
	now := time.Now()
	record.Status = status
	record.ExitedAt = &now
	record.ExitReason = reason
	record.ExitCode = intPtr(exitCode)
	a.ctx.ProcessRegistry.Update(record)
	payload := state.ProcessExitPayload{Name: name, ExitCode: exitCode, Reason: reason}
	if err := a.dispatch(state.Event{Type: state.EventSysProcessExited, Payload: payload}); err != nil {
		// ошибка уже залогирована в dispatch
	}
}

func (a *Application) onAppStopped() {
	a.cleanupOnce.Do(func() { a.runExitCleanup() })
}

func (a *Application) runExitCleanup() {
	if a.ctx == nil {
		return
	}
	if err := a.executeDisconnecting(a.ctx); err != nil {
		a.logger.Errorf("exit cleanup failed: %v", err)
	}
	if a.firewall != nil {
		firewallCtx, cancel := a.requestContext(routeOpTimeout)
		if err := a.firewall.RemoveKillSwitchGroup(firewallCtx); err != nil {
			a.logger.Errorf("exit cleanup firewall group failed: %v", err)
		}
		cancel()
	}
	_ = a.deleteCleanupState()
}

func intPtr(v int) *int {
	ptr := new(int)
	*ptr = v
	return ptr
}
