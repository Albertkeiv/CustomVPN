package ui

import (
	"fmt"
	"image/color"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"customvpn/client/internal/logging"
	"customvpn/client/internal/state"

	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"golang.org/x/text/encoding/charmap"
)

// Options описывает параметры инициализации UI Manager.
type Options struct {
	AppID    string
	AppName  string
	Logger   *logging.Logger
	Dispatch func(state.Event) error
}

// Manager управляет окнами Fyne и связывает их со state machine.
type Manager struct {
	app                     fyne.App
	appName                 string
	logger                  *logging.Logger
	dispatch                func(state.Event) error
	loginWin                fyne.Window
	mainWin                 fyne.Window
	loginWinVisible         bool
	mainWinVisible          bool
	loginEntry              *widget.Entry
	passwordEntry           *widget.Entry
	loginStatus             *widget.Label
	loginBtn                *widget.Button
	retryBtn                *widget.Button
	mainStatus              *widget.Label
	statusCircle            *canvas.Circle
	spinner                 *widget.ProgressBarInfinite
	serverList              *widget.List
	routeList               *widget.List
	servers                 []state.Server
	routes                  []state.RouteProfile
	connectBtn              *widget.Button
	disconnectBtn           *widget.Button
	settingsBtn             *widget.Button
	exitBtn                 *widget.Button
	suppressCredEvents      bool
	suppressServerSelection bool
	suppressRouteSelection  bool
	updateCh                chan uiSnapshot
	stopCh                  chan struct{}
	runOnce                 sync.Once
	shutdownOnce            sync.Once
	wg                      sync.WaitGroup
}

// uiSnapshot переносит срез состояния UI из state machine в goroutine UI.
type uiSnapshot struct {
	LoginVisible        bool
	MainVisible         bool
	IsConnecting        bool
	IsConnected         bool
	SelectedServerID    string
	SelectedProfileID   string
	StatusText          string
	CanLogin            bool
	AllowPreflightRetry bool
	LoginInput          string
	PasswordInput       string
	Servers             []state.Server
	Routes              []state.RouteProfile
}

// NewManager создаёт новый UI Manager.
func NewManager(opts Options) *Manager {
	appID := strings.TrimSpace(opts.AppID)
	if appID == "" {
		appID = "customvpn.client"
	}
	name := strings.TrimSpace(opts.AppName)
	if name == "" {
		name = "CustomVPN"
	}
	fyneApp := fyneapp.NewWithID(appID)
	fyneApp.Settings().SetTheme(newWindows11Theme())
	m := &Manager{
		app:      fyneApp,
		appName:  name,
		logger:   opts.Logger,
		dispatch: opts.Dispatch,
		updateCh: make(chan uiSnapshot, 16),
		stopCh:   make(chan struct{}),
	}
	m.buildLoginWindow()
	m.buildMainWindow()
	return m
}

// Start запускает фоновые goroutine UI и главный цикл Fyne.
func (m *Manager) Start() {
	m.runOnce.Do(func() {
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			m.processUpdates()
		}()
	})
}

// RunMainLoop блокирует текущую горутину до завершения цикла Fyne.
func (m *Manager) RunMainLoop() {
	if m.app == nil {
		return
	}
	m.app.Run()
}

// Shutdown останавливает обновления и закрывает Fyne-приложение.
func (m *Manager) Shutdown() {
	m.shutdownOnce.Do(func() {
		close(m.stopCh)
		m.callOnUI(func() {
			if m.mainWin != nil {
				m.mainWin.Close()
			}
			if m.loginWin != nil {
				m.loginWin.Close()
			}
			m.mainWinVisible = false
			m.loginWinVisible = false
			if m.app != nil {
				m.app.Quit()
			}
		})
	})
}

// WaitAsync ждёт завершения фоновых UI goroutine.
func (m *Manager) WaitAsync(timeout time.Duration) bool {
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

// ShowLoginWindow отображает окно логина.
func (m *Manager) ShowLoginWindow(_ *state.AppContext) {
	m.callOnUI(func() {
		if m.mainWin != nil {
			m.mainWin.Hide()
			m.mainWinVisible = false
		}
		if m.loginWin != nil {
			wasVisible := m.loginWinVisible
			if !wasVisible {
				m.loginWin.Show()
			}
			if !wasVisible && m.loginEntry != nil {
				if canvas := m.loginWin.Canvas(); canvas != nil {
					canvas.Focus(m.loginEntry)
				}
			}
			m.loginWinVisible = true
		}
	})
}

// ShowMainWindow отображает главное окно.
func (m *Manager) ShowMainWindow(_ *state.AppContext) {
	m.callOnUI(func() {
		if m.loginWin != nil {
			m.loginWin.Hide()
			m.loginWinVisible = false
		}
		if m.mainWin != nil {
			m.mainWin.Show()
			m.mainWin.RequestFocus()
			m.mainWinVisible = true
		}
	})
}

// HideMainWindow скрывает главное окно.
func (m *Manager) HideMainWindow(_ *state.AppContext) {
	m.callOnUI(func() {
		if m.mainWin != nil {
			m.mainWin.Hide()
			m.mainWinVisible = false
		}
	})
}

// UpdateUI передаёт снимок состояния UI в безопасную для Fyne goroutine.
func (m *Manager) UpdateUI(ctx *state.AppContext) {
	if ctx == nil {
		return
	}
	snap := uiSnapshot{
		LoginVisible:        ctx.UI.IsLoginVisible,
		MainVisible:         ctx.UI.IsMainVisible,
		IsConnecting:        ctx.UI.IsConnecting,
		IsConnected:         ctx.UI.IsConnected,
		SelectedServerID:    ctx.UI.SelectedServerID,
		SelectedProfileID:   ctx.UI.SelectedProfileID,
		StatusText:          ctx.UI.StatusText,
		CanLogin:            ctx.UI.CanLogin,
		AllowPreflightRetry: ctx.UI.AllowPreflightRetry,
		LoginInput:          ctx.UI.LoginInput,
		PasswordInput:       ctx.UI.PasswordInput,
		Servers:             append([]state.Server(nil), ctx.ServersList...),
		Routes:              append([]state.RouteProfile(nil), ctx.RoutesProfiles...),
	}
	select {
	case <-m.stopCh:
		return
	case m.updateCh <- snap:
	default:
		select {
		case <-m.updateCh:
		default:
		}
		m.updateCh <- snap
	}
}

// ShowModalError отображает модальное окно ошибки.
func (m *Manager) ShowModalError(info *state.ErrorInfo) {
	if info == nil {
		return
	}
	m.callOnUI(func() {
		win := m.activeWindow()
		message := info.UserMessage
		if message == "" {
			message = "Произошла ошибка"
		}
		message = normalizeUserText(message)
		dialog.ShowError(fmt.Errorf(message), win)
		if (info.Kind == state.ErrorKindAuthFailed || info.Kind == state.ErrorKindNetworkUnavailable) && m.loginStatus != nil {
			m.loginStatus.SetText(message)
		}
	})
}

// ShowTransientNotice отображает краткое уведомление.
func (m *Manager) ShowTransientNotice(message string) {
	if strings.TrimSpace(message) == "" {
		return
	}
	m.callOnUI(func() {
		dialog.ShowInformation("CustomVPN", message, m.activeWindow())
	})
}

func (m *Manager) processUpdates() {
	for {
		select {
		case <-m.stopCh:
			return
		case snap := <-m.updateCh:
			m.applySnapshot(snap)
		}
	}
}

func (m *Manager) applySnapshot(snap uiSnapshot) {
	m.callOnUI(func() {
		snap.StatusText = normalizeUserText(snap.StatusText)
		m.updateLoginControls(snap)
		if m.mainStatus != nil {
			m.mainStatus.SetText(snap.StatusText)
		}
		m.updateCredentials(snap.LoginInput, snap.PasswordInput)
		m.updateServers(snap.Servers, snap.SelectedServerID)
		m.updateRoutes(snap.Routes, snap.SelectedProfileID)
		m.updateButtons(snap)
		m.updateStatusIndicator(snap)
	})
}

func (m *Manager) updateCredentials(login, password string) {
	if m.loginEntry == nil || m.passwordEntry == nil {
		return
	}
	m.suppressCredEvents = true
	if m.loginEntry.Text != login {
		m.loginEntry.SetText(login)
	}
	if m.passwordEntry.Text != password {
		m.passwordEntry.SetText(password)
	}
	m.suppressCredEvents = false
}

func (m *Manager) updateServers(list []state.Server, selectedID string) {
	m.servers = list
	if m.serverList == nil {
		return
	}
	m.serverList.Refresh()
	if selectedID == "" {
		m.suppressServerSelection = true
		m.serverList.UnselectAll()
		m.suppressServerSelection = false
		return
	}
	if idx := findServerIndex(list, selectedID); idx >= 0 {
		m.suppressServerSelection = true
		m.serverList.Select(idx)
		m.suppressServerSelection = false
	}
}

func (m *Manager) updateRoutes(list []state.RouteProfile, selectedID string) {
	m.routes = list
	if m.routeList == nil {
		return
	}
	m.routeList.Refresh()
	if selectedID == "" {
		m.suppressRouteSelection = true
		m.routeList.UnselectAll()
		m.suppressRouteSelection = false
		return
	}
	if idx := findRouteIndex(list, selectedID); idx >= 0 {
		m.suppressRouteSelection = true
		m.routeList.Select(idx)
		m.suppressRouteSelection = false
	}
}

func (m *Manager) updateButtons(snap uiSnapshot) {
	if m.connectBtn != nil {
		if snap.MainVisible && !snap.IsConnecting && !snap.IsConnected && snap.SelectedServerID != "" && snap.SelectedProfileID != "" {
			m.connectBtn.Enable()
		} else {
			m.connectBtn.Disable()
		}
	}
	if m.disconnectBtn != nil {
		if snap.MainVisible && (snap.IsConnected || snap.IsConnecting) {
			m.disconnectBtn.Enable()
		} else {
			m.disconnectBtn.Disable()
		}
	}
	if m.settingsBtn != nil {
		if snap.MainVisible && !snap.IsConnecting {
			m.settingsBtn.Enable()
		} else {
			m.settingsBtn.Disable()
		}
	}
}

func (m *Manager) updateStatusIndicator(snap uiSnapshot) {
	if m.statusCircle == nil || m.spinner == nil {
		return
	}
	var fill color.Color
	switch {
	case snap.IsConnected:
		fill = theme.SuccessColor()
	case snap.IsConnecting:
		fill = theme.WarningColor()
	default:
		fill = theme.DisabledColor()
	}
	m.statusCircle.FillColor = fill
	m.statusCircle.Refresh()
	if snap.IsConnecting {
		m.spinner.Show()
		m.spinner.Start()
	} else {
		m.spinner.Stop()
		m.spinner.Hide()
	}
}

func (m *Manager) updateLoginControls(snap uiSnapshot) {
	if m.loginStatus != nil {
		m.loginStatus.SetText(snap.StatusText)
	}
	if m.loginBtn != nil {
		if snap.CanLogin {
			m.loginBtn.Enable()
		} else {
			m.loginBtn.Disable()
		}
	}
	if m.retryBtn != nil {
		if snap.AllowPreflightRetry {
			m.retryBtn.Show()
			m.retryBtn.Enable()
		} else {
			m.retryBtn.Hide()
		}
	}
}

func (m *Manager) buildLoginWindow() {
	if m.app == nil {
		return
	}
	win := m.app.NewWindow(fmt.Sprintf("%s — Вход", m.appName))
	win.Resize(fyne.NewSize(460, 560))
	win.CenterOnScreen()
	win.SetFixedSize(true)

	title := widget.NewLabelWithStyle(m.appName, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	subtitle := widget.NewLabelWithStyle("Авторизация", fyne.TextAlignLeading, fyne.TextStyle{Bold: false})

	m.loginEntry = widget.NewEntry()
	m.loginEntry.SetPlaceHolder("Логин")
	m.loginEntry.OnChanged = func(string) { m.handleCredentialsEdited() }
	m.loginEntry.OnSubmitted = func(string) { m.handleLoginClicked() }

	m.passwordEntry = widget.NewPasswordEntry()
	m.passwordEntry.SetPlaceHolder("Пароль")
	m.passwordEntry.OnChanged = func(string) { m.handleCredentialsEdited() }
	m.passwordEntry.OnSubmitted = func(string) { m.handleLoginClicked() }

	loginButton := widget.NewButton("Войти", m.handleLoginClicked)
	loginButton.Importance = widget.HighImportance
	loginButton.Disable()
	m.loginBtn = loginButton

	m.loginStatus = widget.NewLabel("Проверяем связь с сервером...")
	m.loginStatus.Alignment = fyne.TextAlignLeading
	m.loginStatus.Wrapping = fyne.TextWrapWord

	retryButton := widget.NewButton("Повторить проверку", m.handleRetryPreflight)
	retryButton.Hide()
	m.retryBtn = retryButton

	fields := container.NewVBox(
		widget.NewLabelWithStyle("Логин", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		m.loginEntry,
		widget.NewLabelWithStyle("Пароль", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		m.passwordEntry,
	)
	header := container.NewVBox(title, subtitle)
	form := container.NewVBox(fields, loginButton, layout.NewSpacer())
	statusSlot := canvas.NewRectangle(color.Transparent)
	statusSlot.SetMinSize(fyne.NewSize(0, 72))
	statusBox := container.NewVBox(m.loginStatus, retryButton)
	statusArea := container.NewVBox(widget.NewSeparator(), container.NewMax(statusSlot, statusBox))
	content := container.NewBorder(header, statusArea, nil, nil, form)
	win.SetContent(container.NewPadded(content))
	win.SetCloseIntercept(func() {
		m.handleExitRequested()
	})
	win.Show()
	m.loginWin = win
}

func (m *Manager) buildMainWindow() {
	if m.app == nil {
		return
	}
	win := m.app.NewWindow(m.appName)
	win.Resize(fyne.NewSize(920, 560))
	m.statusCircle = canvas.NewCircle(theme.DisabledColor())
	m.statusCircle.Resize(fyne.NewSize(14, 14))
	m.mainStatus = widget.NewLabel("Отключено")
	m.spinner = widget.NewProgressBarInfinite()
	m.spinner.Hide()

	m.serverList = widget.NewList(
		func() int { return len(m.servers) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			label := obj.(*widget.Label)
			if id < 0 || id >= len(m.servers) {
				label.SetText("—")
				return
			}
			server := m.servers[id]
			country := strings.ToUpper(strings.TrimSpace(server.Country))
			if country == "" {
				country = "?"
			}
			label.SetText(fmt.Sprintf("%s (%s)", server.Name, country))
		},
	)
	m.serverList.OnSelected = m.handleServerSelected

	m.routeList = widget.NewList(
		func() int { return len(m.routes) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			label := obj.(*widget.Label)
			if id < 0 || id >= len(m.routes) {
				label.SetText("—")
				return
			}
			profile := m.routes[id]
			label.SetText(profile.Name)
		},
	)
	m.routeList.OnSelected = m.handleRouteSelected

	serversCard := widget.NewCard("Серверы", "", container.NewMax(m.serverList))
	routesCard := widget.NewCard("Профили маршрутов", "", container.NewMax(m.routeList))
	split := container.NewHSplit(serversCard, routesCard)
	split.SetOffset(0.5)

	statusBar := container.NewHBox(
		m.statusCircle,
		widget.NewLabel("Статус:"),
		m.mainStatus,
		layout.NewSpacer(),
		m.spinner,
	)

	m.connectBtn = widget.NewButton("Подключиться", func() { m.sendSimpleEvent(state.EventUIClickConnect) })
	m.disconnectBtn = widget.NewButton("Отключиться", func() { m.sendSimpleEvent(state.EventUIClickDisconnect) })
	m.settingsBtn = widget.NewButton("Настройки", func() { m.sendSimpleEvent(state.EventUIOpenSettings) })
	m.exitBtn = widget.NewButton("Выход", func() { m.sendSimpleEvent(state.EventUIExit) })

	controls := container.NewGridWithColumns(4, m.connectBtn, m.disconnectBtn, m.settingsBtn, m.exitBtn)
	mainContent := container.NewBorder(statusBar, controls, nil, nil, split)
	win.SetContent(container.NewPadded(mainContent))
	win.SetCloseIntercept(func() {
		m.sendSimpleEvent(state.EventUICloseWindow)
		win.Hide()
	})
	win.Hide()
	m.mainWin = win
}

func (m *Manager) handleLoginClicked() {
	if m.loginEntry == nil || m.passwordEntry == nil {
		m.sendSimpleEvent(state.EventUIClickLogin)
		return
	}
	payload := state.CredentialsPayload{
		Login:    m.loginEntry.Text,
		Password: m.passwordEntry.Text,
	}
	evt := state.Event{Type: state.EventUIClickLogin, Payload: payload, TS: time.Now()}
	m.dispatchEvent(evt)
}

func (m *Manager) handleCredentialsEdited() {
	if m.suppressCredEvents {
		return
	}
	payload := state.CredentialsPayload{
		Login:    m.loginEntry.Text,
		Password: m.passwordEntry.Text,
	}
	evt := state.Event{Type: state.EventUICredentialsChanged, Payload: payload, TS: time.Now()}
	m.dispatchEvent(evt)
}

func (m *Manager) handleServerSelected(id widget.ListItemID) {
	if m.suppressServerSelection {
		return
	}
	if id < 0 || int(id) >= len(m.servers) {
		return
	}
	server := m.servers[id]
	payload := state.SelectionPayload{ID: server.ID}
	evt := state.Event{Type: state.EventUISelectServer, Payload: payload, TS: time.Now()}
	m.dispatchEvent(evt)
}

func (m *Manager) handleRouteSelected(id widget.ListItemID) {
	if m.suppressRouteSelection {
		return
	}
	if id < 0 || int(id) >= len(m.routes) {
		return
	}
	profile := m.routes[id]
	payload := state.SelectionPayload{ID: profile.ID}
	evt := state.Event{Type: state.EventUISelectRoute, Payload: payload, TS: time.Now()}
	m.dispatchEvent(evt)
}

func (m *Manager) handleExitRequested() {
	m.sendSimpleEvent(state.EventUIExit)
}

func (m *Manager) handleRetryPreflight() {
	m.sendSimpleEvent(state.EventUIClickRetryPreflight)
}

func (m *Manager) sendSimpleEvent(t state.EventType) {
	evt := state.Event{Type: t, TS: time.Now()}
	m.dispatchEvent(evt)
}

func (m *Manager) dispatchEvent(evt state.Event) {
	if m.dispatch == nil {
		return
	}
	if err := m.dispatch(evt); err != nil && m.logger != nil {
		m.logger.Errorf("ui dispatch %s failed: %v", evt.Type, err)
	}
}

func (m *Manager) activeWindow() fyne.Window {
	if m.loginWinVisible && m.loginWin != nil {
		return m.loginWin
	}
	if m.mainWinVisible && m.mainWin != nil {
		return m.mainWin
	}
	if m.loginWin != nil {
		return m.loginWin
	}
	return m.mainWin
}

func (m *Manager) callOnUI(fn func()) {
	if m.app == nil || fn == nil {
		return
	}
	if drv := m.app.Driver(); drv != nil {
		drv.DoFromGoroutine(fn, true)
		return
	}
	fn()
}

func normalizeUserText(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return message
	}
	encoded, err := charmap.Windows1251.NewEncoder().Bytes([]byte(message))
	if err != nil {
		return message
	}
	if utf8.Valid(encoded) {
		fixed := string(encoded)
		if fixed != "" {
			return fixed
		}
	}
	return message
}

func findServerIndex(list []state.Server, id string) int {
	for i, srv := range list {
		if srv.ID == id {
			return i
		}
	}
	return -1
}

func findRouteIndex(list []state.RouteProfile, id string) int {
	for i, profile := range list {
		if profile.ID == id {
			return i
		}
	}
	return -1
}
