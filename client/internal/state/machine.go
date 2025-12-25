package state

import (
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"customvpn/client/internal/logging"
)

// State описывает состояние конечного автомата приложения.
type State string

const (
	StateAppStarting       State = "AppStarting"
	StatePreflightCheck    State = "PreflightCheck"
	StateWaitingLogin      State = "WaitingLogin"
	StateAuthInProgress    State = "AuthInProgress"
	StateSyncInProgress    State = "SyncInProgress"
	StatePreparingEnv      State = "PreparingEnvironment"
	StateReadyDisconnected State = "ReadyDisconnected"
	StateConnecting        State = "Connecting"
	StateConnected         State = "Connected"
	StateDisconnecting     State = "Disconnecting"
	StateError             State = "Error"
	StateExiting           State = "Exiting"
)

// EventType представляет собой тип события из очереди state machine.
type EventType string

const (
	EventUILaunch              EventType = "UI_LAUNCH"
	EventUICredentialsChanged  EventType = "UI_CREDENTIALS_CHANGED"
	EventUIClickLogin          EventType = "UI_CLICK_LOGIN"
	EventUIClickRetryPreflight EventType = "UI_CLICK_RETRY_PREFLIGHT"
	EventUISelectProfile       EventType = "UI_SELECT_PROFILE"
	EventUIClickConnect        EventType = "UI_CLICK_CONNECT"
	EventUIClickDisconnect     EventType = "UI_CLICK_DISCONNECT"
	EventUIClickCleanup        EventType = "UI_CLICK_CLEANUP"
	EventUIOpenSettings        EventType = "UI_OPEN_SETTINGS"
	EventUICloseWindow         EventType = "UI_CLOSE_WINDOW"
	EventUIShowWindow          EventType = "UI_SHOW_WINDOW"
	EventUIExit                EventType = "UI_EXIT"

	EventTrayShowWindow EventType = "TRAY_SHOW_WINDOW"
	EventTrayHideWindow EventType = "TRAY_HIDE_WINDOW"
	EventTrayConnect    EventType = "TRAY_CONNECT"
	EventTrayDisconnect EventType = "TRAY_DISCONNECT"
	EventTrayExit       EventType = "TRAY_EXIT"

	EventSysPreflightSuccess  EventType = "SYS_PREFLIGHT_SUCCESS"
	EventSysPreflightFailure  EventType = "SYS_PREFLIGHT_FAILURE"
	EventSysPreflightRetry    EventType = "SYS_PREFLIGHT_RETRY"
	EventSysAuthSuccess       EventType = "SYS_AUTH_SUCCESS"
	EventSysAuthFailure       EventType = "SYS_AUTH_FAILURE"
	EventSysSyncSuccess       EventType = "SYS_SYNC_SUCCESS"
	EventSysSyncFailure       EventType = "SYS_SYNC_FAILURE"
	EventSysPrepareEnvSuccess EventType = "SYS_PREPARE_ENV_SUCCESS"
	EventSysPrepareEnvFailure EventType = "SYS_PREPARE_ENV_FAILURE"
	EventSysConnectingSuccess EventType = "SYS_CONNECTING_SUCCESS"
	EventSysConnectingFailure EventType = "SYS_CONNECTING_FAILURE"
	EventSysDisconnectingDone EventType = "SYS_DISCONNECTING_DONE"
	EventSysProcessExited     EventType = "SYS_PROCESS_EXITED"
	EventSysCleanupDone       EventType = "SYS_CLEANUP_DONE"
	EventSysTimeout           EventType = "SYS_TIMEOUT"
)

const preflightRetryDelay = 5 * time.Second

// Event инкапсулирует событие очереди и произвольную полезную нагрузку.
type Event struct {
	Type    EventType
	Payload any
	TS      time.Time
}

// CredentialsPayload передаёт логин/пароль из UI.
type CredentialsPayload struct {
	Login    string
	Password string
}

// SelectionPayload используется для изменения выбранных ID.
type SelectionPayload struct {
	ID string
}

// AuthSuccessPayload содержит authToken.
type AuthSuccessPayload struct {
	Token string
}

// SyncSuccessPayload содержит списки серверов и профилей.
type SyncSuccessPayload struct {
	Profiles []Profile
}

// PrepareEnvSuccessPayload содержит найденный default gateway.
type PrepareEnvSuccessPayload struct {
	Gateway GatewayInfo
}

// ScenarioResultPayload описывает успех/ошибку длительных процедур.
type ScenarioResultPayload struct {
	Kind             ErrorKind
	Message          string
	TechnicalMessage string
}

// ProcessExitPayload сообщает о завершении дочернего процесса.
type ProcessExitPayload struct {
	Name     ProcessName
	ExitCode int
	Reason   string
}

// CleanupResultPayload reports cleanup completion details.
type CleanupResultPayload struct {
	Errors []string
}

// TimeoutPayload описывает операцию, превысившую таймаут.
type TimeoutPayload struct {
	Operation string
}

// Callbacks содержит функции, вызываемые state machine для побочных эффектов.
type Callbacks struct {
	StartPreflight      func(ctx *AppContext)
	StartAuth           func(ctx *AppContext, login, password string)
	StartSync           func(ctx *AppContext)
	StartPrepareEnv     func(ctx *AppContext)
	StartConnecting     func(ctx *AppContext)
	StartDisconnecting  func(ctx *AppContext)
	ForceCleanup        func(ctx *AppContext)
	CleanupAndExit      func(ctx *AppContext)
	ShowLoginWindow     func(ctx *AppContext)
	ShowMainWindow      func(ctx *AppContext)
	HideMainWindow      func(ctx *AppContext)
	UpdateUI            func(ctx *AppContext)
	ShowModalError      func(info *ErrorInfo)
	ShowTransientNotice func(message string)
	ShowCleanupStarted  func()
	ShowCleanupDone     func(hasErrors bool)
}

// Machine инкапсулирует event-loop и текущее состояние приложения.
type Machine struct {
	ctx                 *AppContext
	callbacks           Callbacks
	logger              *logging.Logger
	events              chan Event
	priority            chan Event
	done                chan struct{}
	stopped             atomic.Bool
	loopOnce            sync.Once
	stopOnce            sync.Once
	wg                  sync.WaitGroup
	pendingPF           bool
	preflightRetryTimer *time.Timer
}

// ErrMachineStopped возвращается при попытке отправить событие после остановки петли.
var ErrMachineStopped = errors.New("state machine stopped")

// NewMachine создаёт новый state machine в состоянии AppStarting.
func NewMachine(ctx *AppContext, logger *logging.Logger, callbacks Callbacks) *Machine {
	return &Machine{
		ctx:       ctx,
		callbacks: callbacks,
		logger:    logger,
		events:    make(chan Event, 64),
		priority:  make(chan Event, 8),
		done:      make(chan struct{}),
	}
}

// Start запускает event-loop в отдельной горутине.
func (m *Machine) Start() {
	m.loopOnce.Do(func() {
		go m.loopSafely()
	})
}

// Stop завершает event-loop.
func (m *Machine) Stop() {
	m.stopOnce.Do(func() {
		m.cancelPreflightRetry()
		m.stopped.Store(true)
		close(m.done)
		close(m.priority)
		close(m.events)
	})
}

// WaitAsync ждёт завершения фоновых задач, запущенных state machine.
func (m *Machine) WaitAsync(timeout time.Duration) bool {
	if m == nil {
		return true
	}
	if timeout <= 0 {
		m.wg.Wait()
		return true
	}
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// Dispatch отправляет событие в очередь state machine.
func (m *Machine) Dispatch(evt Event) error {
	if m.stopped.Load() {
		return ErrMachineStopped
	}
	if m.logger != nil {
		m.logger.Debugf("event queued: %s", evt.Type)
	}
	ch := m.events
	if m.isExitEvent(evt.Type) {
		ch = m.priority
	}
	select {
	case <-m.done:
		return ErrMachineStopped
	case ch <- evt:
		return nil
	default:
		// если канал заполнен, блокируемся, пока возможно отправить
		if m.stopped.Load() {
			return ErrMachineStopped
		}
		if m.safeSend(ch, evt) {
			return nil
		}
		return ErrMachineStopped
	}
}

func (m *Machine) loop() {
	for {
		if m.stopped.Load() {
			return
		}

		select {
		case evt, ok := <-m.priority:
			if !ok {
				return
			}
			m.handleEvent(evt)
			continue
		default:
		}

		select {
		case evt, ok := <-m.priority:
			if !ok {
				return
			}
			m.handleEvent(evt)
		case evt, ok := <-m.events:
			if !ok {
				return
			}
			m.handleEvent(evt)
		}
	}
}

func (m *Machine) loopSafely() {
	defer m.logPanic("state loop")
	m.loop()
}

func (m *Machine) handleEvent(evt Event) {
	if evt.TS.IsZero() {
		evt.TS = time.Now()
	}
	if m.logger != nil {
		m.logger.Debugf("event handle: %s state=%s", evt.Type, m.ctx.State)
	}
	if evt.Type == EventUIClickCleanup {
		if m.callbacks.ShowCleanupStarted != nil {
			m.callbacks.ShowCleanupStarted()
		} else {
			m.showTransient("Очистка запущена")
		}
		m.invokeForceCleanup()
		return
	}
	if m.isExitEvent(evt.Type) {
		m.transition(StateExiting)
		m.invokeCleanup()
		return
	}

	switch m.ctx.State {
	case StateAppStarting:
		m.handleAppStarting(evt)
	case StatePreflightCheck:
		m.handlePreflight(evt)
	case StateWaitingLogin:
		m.handleWaitingLogin(evt)
	case StateAuthInProgress:
		m.handleAuthInProgress(evt)
	case StateSyncInProgress:
		m.handleSyncInProgress(evt)
	case StatePreparingEnv:
		m.handlePreparingEnv(evt)
	case StateReadyDisconnected:
		m.handleReady(evt)
	case StateConnecting:
		m.handleConnecting(evt)
	case StateConnected:
		m.handleConnected(evt)
	case StateDisconnecting:
		m.handleDisconnecting(evt)
	case StateError:
		m.handleErrorState(evt)
	case StateExiting:
		// игнор
	default:
		m.logger.Debugf("state machine: unknown state %s", m.ctx.State)
	}
	if evt.Type == EventSysCleanupDone {
		payload, _ := evt.Payload.(CleanupResultPayload)
		if m.callbacks.ShowCleanupDone != nil {
			m.callbacks.ShowCleanupDone(len(payload.Errors) > 0)
			return
		}
		if len(payload.Errors) == 0 {
			m.showTransient("Очистка завершена")
		} else {
			m.showTransient("Очистка завершена с ошибками")
		}
	}
}

func (m *Machine) handleAppStarting(evt Event) {
	switch evt.Type {
	case EventUILaunch:
		m.ctx.UI.StatusText = "Проверяем доступность сервера..."
		m.transition(StatePreflightCheck)
		m.invokePreflight()
	case EventUICredentialsChanged:
		m.applyCredentials(evt)
	default:
		m.logger.Debugf("appStarting: ignored %s", evt.Type)
	}
}

func (m *Machine) handlePreflight(evt Event) {
	switch evt.Type {
	case EventSysPreflightSuccess:
		m.cancelPreflightRetry()
		m.ctx.UI.StatusText = "Введите логин и пароль"
		m.transition(StateWaitingLogin)
		m.invokeShowLogin()
	case EventSysPreflightFailure:
		payload, _ := evt.Payload.(ScenarioResultPayload)
		m.onPreflightFailure(payload)
	case EventUIClickRetryPreflight:
		m.handlePreflightRetry(true)
	case EventSysPreflightRetry:
		m.handlePreflightRetry(false)
	case EventUICredentialsChanged:
		m.applyCredentials(evt)
	default:
		m.logger.Debugf("preflight: ignored %s", evt.Type)
	}
}

func (m *Machine) handleWaitingLogin(evt Event) {
	switch evt.Type {
	case EventUICredentialsChanged:
		m.applyCredentials(evt)
	case EventUIClickLogin:
		m.applyCredentials(evt)
		if strings.TrimSpace(m.ctx.UI.LoginInput) == "" || strings.TrimSpace(m.ctx.UI.PasswordInput) == "" {
			m.showTransient("Укажите логин и пароль")
			return
		}
		m.ctx.UI.StatusText = "Выполняется авторизация"
		m.transition(StateAuthInProgress)
		m.invokeAuth()
	case EventUICloseWindow:
		m.invokeHideMain()
	case EventUIShowWindow, EventTrayShowWindow:
		m.invokeShowLogin()
	default:
		m.logger.Debugf("waitingLogin: ignored %s", evt.Type)
	}
}

func (m *Machine) handleAuthInProgress(evt Event) {
	switch evt.Type {
	case EventSysAuthSuccess:
		payload, _ := evt.Payload.(AuthSuccessPayload)
		m.ctx.AuthToken = payload.Token
		m.ctx.LastError = nil
		m.ctx.UI.StatusText = "Обновление списков серверов"
		m.transition(StateSyncInProgress)
		m.invokeSync()
	case EventSysAuthFailure:
		payload, _ := evt.Payload.(ScenarioResultPayload)
		kind := payload.Kind
		if kind == "" {
			kind = ErrorKindAuthFailed
		}
		message := payload.Message
		if message == "" {
			message = "Ошибка авторизации"
		}
		technical := payload.TechnicalMessage
		if technical == "" {
			technical = "auth failed"
		}
		m.enterError(kind, message, technical)
	default:
		m.logger.Debugf("auth: ignored %s", evt.Type)
	}
}

func (m *Machine) handleSyncInProgress(evt Event) {
	switch evt.Type {
	case EventSysSyncSuccess:
		payload, _ := evt.Payload.(SyncSuccessPayload)
		m.ctx.Profiles = payload.Profiles
		m.ctx.UI.StatusText = "Подготовка окружения"
		m.transition(StatePreparingEnv)
		m.invokePrepareEnv()
	case EventSysSyncFailure:
		payload, _ := evt.Payload.(ScenarioResultPayload)
		kind := payload.Kind
		if kind == "" {
			kind = ErrorKindSyncFailed
		}
		message := payload.Message
		if message == "" {
			message = "Не удалось загрузить данные"
		}
		technical := payload.TechnicalMessage
		if technical == "" {
			technical = "sync failed"
		}
		m.enterError(kind, message, technical)
	default:
		m.logger.Debugf("sync: ignored %s", evt.Type)
	}
}

func (m *Machine) handlePreparingEnv(evt Event) {
	switch evt.Type {
	case EventSysPrepareEnvSuccess:
		payload, _ := evt.Payload.(PrepareEnvSuccessPayload)
		gw := payload.Gateway
		if strings.TrimSpace(gw.IP) != "" {
			m.ctx.DefaultGateway = &gw
		} else {
			m.ctx.DefaultGateway = nil
		}
		m.ctx.UI.StatusText = "Отключено"
		m.transition(StateReadyDisconnected)
		m.invokeShowMain()
	case EventSysPrepareEnvFailure:
		payload, _ := evt.Payload.(ScenarioResultPayload)
		kind := payload.Kind
		if kind == "" {
			kind = ErrorKindRoutingFailed
		}
		message := payload.Message
		if message == "" {
			message = "Не удалось подготовить маршруты"
		}
		technical := payload.TechnicalMessage
		if technical == "" {
			technical = "prepare env failed"
		}
		m.enterError(kind, message, technical)
	default:
		m.logger.Debugf("prepareEnv: ignored %s", evt.Type)
	}
}

func (m *Machine) handleReady(evt Event) {
	switch evt.Type {
	case EventUISelectProfile:
		m.applyProfileSelection(evt)
	case EventUIClickConnect, EventTrayConnect:
		if m.ctx.SelectedProfileID == "" {
			m.showTransient("Выберите профиль")
			return
		}
		m.pendingPF = false
		m.ctx.UI.StatusText = "Подключение..."
		m.transition(StateConnecting)
		m.invokeConnect()
	case EventUICloseWindow, EventTrayHideWindow:
		m.invokeHideMain()
	case EventUIShowWindow, EventTrayShowWindow:
		m.invokeShowMain()
	case EventUIOpenSettings:
		m.logger.Debugf("settings dialog requested")
	default:
		m.logger.Debugf("ready: ignored %s", evt.Type)
	}
}

func (m *Machine) handleConnecting(evt Event) {
	switch evt.Type {
	case EventSysConnectingSuccess:
		m.ctx.UI.StatusText = "Подключено"
		m.transition(StateConnected)
	case EventSysConnectingFailure:
		payload, _ := evt.Payload.(ScenarioResultPayload)
		kind := payload.Kind
		if kind == "" {
			kind = ErrorKindProcessFailed
		}
		message := payload.Message
		if message == "" {
			message = "Не удалось подключиться"
		}
		m.enterError(kind, message, "connecting failed")
	case EventSysProcessExited:
		payload, _ := evt.Payload.(ProcessExitPayload)
		m.enterError(ErrorKindProcessFailed, "Процесс завершился во время подключения", payload.Reason)
	default:
		m.logger.Debugf("connecting: ignored %s", evt.Type)
	}
}

func (m *Machine) handleConnected(evt Event) {
	switch evt.Type {
	case EventUISelectProfile:
		m.applyProfileSelection(evt)
	case EventUIClickDisconnect, EventTrayDisconnect:
		m.pendingPF = false
		m.ctx.UI.StatusText = "Отключение..."
		m.transition(StateDisconnecting)
		m.invokeDisconnect()
	case EventSysProcessExited:
		payload, _ := evt.Payload.(ProcessExitPayload)
		m.pendingPF = true
		m.ctx.UI.StatusText = "Отключение..."
		m.transition(StateDisconnecting)
		m.invokeDisconnect()
		m.ctx.LastError = &ErrorInfo{
			Kind:             ErrorKindProcessFailed,
			UserMessage:      "Процесс завершился неожиданно",
			TechnicalMessage: payload.Reason,
			OccurredAt:       time.Now(),
		}
	case EventSysTimeout:
		payload, _ := evt.Payload.(TimeoutPayload)
		m.enterError(ErrorKindUnknown, fmt.Sprintf("Таймаут операции %s", payload.Operation), "timeout in connected")
	default:
		m.logger.Debugf("connected: ignored %s", evt.Type)
	}
}

func (m *Machine) handleDisconnecting(evt Event) {
	switch evt.Type {
	case EventUISelectProfile:
		m.applyProfileSelection(evt)
	case EventSysDisconnectingDone:
		m.ctx.UI.StatusText = "Отключено"
		m.transition(StateReadyDisconnected)
		if m.pendingPF {
			m.pendingPF = false
			m.enterError(ErrorKindProcessFailed, "Процесс завершился с ошибкой", "process crashed")
		}
	default:
		m.logger.Debugf("disconnecting: ignored %s", evt.Type)
	}
}

func (m *Machine) handleErrorState(evt Event) {
	if evt.Type == EventUICredentialsChanged {
		m.applyCredentials(evt)
		return
	}
	if evt.Type == EventUISelectProfile {
		m.applyProfileSelection(evt)
		return
	}
	if evt.Type == EventUIClickLogin && m.ctx.LastError != nil {
		m.applyCredentials(evt)
		m.ctx.UI.StatusText = "Выполняется авторизация"
		m.transition(StateAuthInProgress)
		m.invokeAuth()
		return
	}
	if (evt.Type == EventUIClickConnect || evt.Type == EventTrayConnect) && m.ctx.LastError != nil && (m.ctx.LastError.Kind == ErrorKindProcessFailed || m.ctx.LastError.Kind == ErrorKindRoutingFailed) {
		if m.ctx.SelectedProfileID == "" {
			m.showTransient("Выберите профиль")
			return
		}
		m.ctx.UI.StatusText = "Подключение..."
		m.transition(StateConnecting)
		m.invokeConnect()
		return
	}
	if evt.Type == EventTrayShowWindow || evt.Type == EventUIShowWindow {
		m.invokeShowMain()
	}
}

func (m *Machine) applyCredentials(evt Event) {
	if payload, ok := evt.Payload.(CredentialsPayload); ok {
		m.ctx.UI.LoginInput = payload.Login
		m.ctx.UI.PasswordInput = payload.Password
	}
}

func (m *Machine) applyProfileSelection(evt Event) {
	if payload, ok := evt.Payload.(SelectionPayload); ok {
		m.ctx.SelectedProfileID = payload.ID
		m.ctx.UI.SelectedProfileID = payload.ID
		m.refreshUI()
	}
}

func (m *Machine) transition(next State) {
	if m.ctx.State == next {
		return
	}
	prev := m.ctx.State
	m.ctx.State = next
	m.logger.Debugf("state transition %s → %s", prev, next)
	m.updateUIForState(next)
}

func (m *Machine) updateUIForState(state State) {
	m.ctx.UI.CanLogin = false
	m.ctx.UI.AllowPreflightRetry = false
	switch state {
	case StateWaitingLogin:
		m.ctx.UI.IsLoginVisible = true
		m.ctx.UI.IsMainVisible = false
		m.ctx.UI.CanLogin = true
	case StateReadyDisconnected:
		m.ctx.UI.IsLoginVisible = false
		m.ctx.UI.IsMainVisible = true
		m.ctx.UI.IsConnecting = false
		m.ctx.UI.IsConnected = false
	case StateConnecting:
		m.ctx.UI.IsConnecting = true
	case StateConnected:
		m.ctx.UI.IsConnecting = false
		m.ctx.UI.IsConnected = true
	case StateDisconnecting:
		m.ctx.UI.IsConnecting = false
	case StateError:
		m.ctx.UI.IsConnecting = false
		if m.ctx.LastError != nil {
			m.ctx.UI.CanLogin = true
		}
	}
	m.refreshUI()
}

func (m *Machine) enterError(kind ErrorKind, userMessage, technical string) {
	info := &ErrorInfo{
		Kind:             kind,
		UserMessage:      userMessage,
		TechnicalMessage: technical,
		OccurredAt:       time.Now(),
	}
	m.ctx.LastError = info
	m.ctx.UI.StatusText = userMessage
	m.transition(StateError)
	if m.callbacks.ShowModalError != nil {
		m.callbacks.ShowModalError(info)
	}
}

func (m *Machine) invokePreflight() {
	if m.callbacks.StartPreflight != nil {
		m.runAsync(func() { m.callbacks.StartPreflight(m.ctx) })
	}
}

func (m *Machine) invokeAuth() {
	if m.callbacks.StartAuth != nil {
		login := m.ctx.UI.LoginInput
		password := m.ctx.UI.PasswordInput
		m.runAsync(func() { m.callbacks.StartAuth(m.ctx, login, password) })
	}
}

func (m *Machine) invokeSync() {
	if m.callbacks.StartSync != nil {
		m.runAsync(func() { m.callbacks.StartSync(m.ctx) })
	}
}

func (m *Machine) invokePrepareEnv() {
	if m.callbacks.StartPrepareEnv != nil {
		m.runAsync(func() { m.callbacks.StartPrepareEnv(m.ctx) })
	}
}

func (m *Machine) invokeConnect() {
	if m.callbacks.StartConnecting != nil {
		m.runAsync(func() { m.callbacks.StartConnecting(m.ctx) })
	}
}

func (m *Machine) invokeDisconnect() {
	if m.callbacks.StartDisconnecting != nil {
		m.runAsync(func() { m.callbacks.StartDisconnecting(m.ctx) })
	}
}

func (m *Machine) invokeForceCleanup() {
	if m.callbacks.ForceCleanup != nil {
		m.runAsync(func() { m.callbacks.ForceCleanup(m.ctx) })
	}
}

func (m *Machine) runAsync(fn func()) {
	if fn == nil {
		return
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer m.logPanic("async task")
		fn()
	}()
}

func (m *Machine) logPanic(scope string) {
	if r := recover(); r != nil {
		if m.logger != nil {
			m.logger.Errorf("panic in %s: %v\n%s", scope, r, debug.Stack())
		}
		panic(r)
	}
}

func (m *Machine) invokeCleanup() {
	if m.callbacks.CleanupAndExit != nil {
		m.callbacks.CleanupAndExit(m.ctx)
		return
	}
	if !m.stopped.Load() {
		m.Stop()
	}
}

func (m *Machine) invokeShowLogin() {
	if m.callbacks.ShowLoginWindow != nil {
		m.callbacks.ShowLoginWindow(m.ctx)
	}
}

func (m *Machine) invokeShowMain() {
	if m.callbacks.ShowMainWindow != nil {
		m.callbacks.ShowMainWindow(m.ctx)
	}
}

func (m *Machine) invokeHideMain() {
	if m.callbacks.HideMainWindow != nil {
		m.callbacks.HideMainWindow(m.ctx)
	}
}

func (m *Machine) showTransient(message string) {
	if m.callbacks.ShowTransientNotice != nil {
		m.callbacks.ShowTransientNotice(message)
	} else {
		m.logger.Infof("notice: %s", message)
	}
}

func (m *Machine) onPreflightFailure(payload ScenarioResultPayload) {
	message := strings.TrimSpace(payload.Message)
	if message == "" {
		message = "Нет связи с управляющим сервером. Повторим через 5 секунд"
	}
	m.ctx.UI.StatusText = message
	m.ctx.UI.AllowPreflightRetry = true
	m.ctx.UI.CanLogin = false
	m.ctx.UI.IsLoginVisible = true
	m.ctx.UI.IsMainVisible = false
	m.refreshUI()
	m.invokeShowLogin()
	m.schedulePreflightRetry(preflightRetryDelay)
}

func (m *Machine) handlePreflightRetry(manual bool) {
	m.cancelPreflightRetry()
	m.ctx.UI.AllowPreflightRetry = false
	m.ctx.UI.CanLogin = false
	if manual {
		m.ctx.UI.StatusText = "Повторяем проверку..."
	} else {
		m.ctx.UI.StatusText = "Повторяем проверку соединения..."
	}
	m.refreshUI()
	m.invokePreflight()
}

func (m *Machine) schedulePreflightRetry(delay time.Duration) {
	if delay <= 0 {
		delay = preflightRetryDelay
	}
	m.cancelPreflightRetry()
	m.preflightRetryTimer = time.AfterFunc(delay, func() {
		_ = m.Dispatch(Event{Type: EventSysPreflightRetry})
	})
}

func (m *Machine) cancelPreflightRetry() {
	if m.preflightRetryTimer != nil {
		m.preflightRetryTimer.Stop()
		m.preflightRetryTimer = nil
	}
}

func (m *Machine) refreshUI() {
	if m.callbacks.UpdateUI != nil {
		m.callbacks.UpdateUI(m.ctx)
	}
}

func (m *Machine) isExitEvent(t EventType) bool {
	return t == EventTrayExit || t == EventUIExit
}

func (m *Machine) safeSend(ch chan Event, evt Event) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	ch <- evt
	return true
}
