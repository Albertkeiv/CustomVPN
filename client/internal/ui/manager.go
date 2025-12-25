package ui

import (
	"fmt"
	"image/color"
	"runtime/debug"
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
	"fyne.io/fyne/v2/lang"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"fyne.io/systray"
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
	profileList             *widget.List
	profiles                []state.Profile
	connectBtn              *widget.Button
	disconnectBtn           *widget.Button
	settingsBtn             *widget.Button
	exitBtn                 *widget.Button
	cleanupDialog           *dialog.CustomDialog
	cleanupDialogLabel      *widget.Label
	cleanupDialogButton     *widget.Button
	cleanupDialogParent     fyne.Window
	suppressCredEvents      bool
	suppressProfileSelect   bool
	updateCh                chan uiSnapshot
	stopCh                  chan struct{}
	runOnce                 sync.Once
	shutdownOnce            sync.Once
	wg                      sync.WaitGroup
	lastShownLogin          bool
}

// uiSnapshot переносит срез состояния UI из state machine в goroutine UI.
type uiSnapshot struct {
	LoginVisible        bool
	MainVisible         bool
	IsConnecting        bool
	IsConnected         bool
	SelectedProfileID   string
	StatusText          string
	CanLogin            bool
	AllowPreflightRetry bool
	LoginInput          string
	PasswordInput       string
	Profiles            []state.Profile
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
		lastShownLogin: true,
	}
	m.buildLoginWindow()
	m.buildMainWindow()
	m.setupTray()
	return m
}

// Start запускает фоновые goroutine UI и главный цикл Fyne.
func (m *Manager) Start() {
	m.runOnce.Do(func() {
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			defer m.logPanic("ui updates")
			m.processUpdates()
		}()
	})
}

// SetOnStopped registers a callback fired when the app stops.
func (m *Manager) SetOnStopped(fn func()) {
	if m == nil || m.app == nil {
		return
	}
	m.app.Lifecycle().SetOnStopped(fn)
}

// Quit requests the app to exit.
func (m *Manager) Quit() {
	if m == nil || m.app == nil {
		return
	}
	m.callOnUI(func() {
		m.app.Quit()
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
			m.lastShownLogin = true
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
			m.lastShownLogin = false
		}
	})
}

// HideMainWindow скрывает главное окно.
func (m *Manager) HideMainWindow(_ *state.AppContext) {
	m.callOnUI(func() {
		if m.loginWin != nil {
			m.loginWin.Hide()
			m.loginWinVisible = false
		}
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
		SelectedProfileID:   ctx.UI.SelectedProfileID,
		StatusText:          ctx.UI.StatusText,
		CanLogin:            ctx.UI.CanLogin,
		AllowPreflightRetry: ctx.UI.AllowPreflightRetry,
		LoginInput:          ctx.UI.LoginInput,
		PasswordInput:       ctx.UI.PasswordInput,
		Profiles:            append([]state.Profile(nil), ctx.Profiles...),
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

// ConfirmEnableLocalPolicyMerge asks the user to allow local firewall rules.
func (m *Manager) ConfirmEnableLocalPolicyMerge() bool {
	return m.confirmDialog(
		"Kill Switch",
		"В системе запрещены локальные правила брандмауэра. Разрешить их сейчас? Для этого нужны права администратора.",
	)
}

// ShowCleanupStarted shows a single cleanup dialog without an enabled close button.
func (m *Manager) ShowCleanupStarted() {
	m.callOnUI(func() {
		m.ensureCleanupDialog()
		if m.cleanupDialogLabel != nil {
			m.cleanupDialogLabel.SetText("Очистка начата")
		}
		if m.cleanupDialogButton != nil {
			m.cleanupDialogButton.Disable()
		}
		if m.cleanupDialog != nil {
			m.cleanupDialog.Show()
		}
	})
}

// ShowCleanupDone updates the cleanup dialog to a finished state.
func (m *Manager) ShowCleanupDone(hasErrors bool) {
	m.callOnUI(func() {
		m.ensureCleanupDialog()
		if m.cleanupDialogLabel != nil {
			if hasErrors {
				m.cleanupDialogLabel.SetText("Очистка завершена с ошибками")
			} else {
				m.cleanupDialogLabel.SetText("Очистка завершена")
			}
		}
		if m.cleanupDialogButton != nil {
			m.cleanupDialogButton.Enable()
		}
		if m.cleanupDialog != nil {
			m.cleanupDialog.Show()
		}
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

func (m *Manager) logPanic(scope string) {
	if r := recover(); r != nil {
		if m.logger != nil {
			m.logger.Errorf("panic in %s: %v\n%s", scope, r, debug.Stack())
		}
		panic(r)
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
		m.updateProfiles(snap.Profiles, snap.SelectedProfileID)
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

func (m *Manager) updateProfiles(list []state.Profile, selectedID string) {
	m.profiles = list
	if m.profileList == nil {
		return
	}
	m.profileList.Refresh()
	if selectedID == "" {
		m.suppressProfileSelect = true
		m.profileList.UnselectAll()
		m.suppressProfileSelect = false
		return
	}
	if idx := findProfileIndex(list, selectedID); idx >= 0 {
		m.suppressProfileSelect = true
		m.profileList.Select(idx)
		m.suppressProfileSelect = false
	}
}

func (m *Manager) updateButtons(snap uiSnapshot) {
	if m.connectBtn != nil {
		if snap.MainVisible && !snap.IsConnecting && !snap.IsConnected && snap.SelectedProfileID != "" {
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
		m.settingsBtn.Disable()
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
	cleanupButton := widget.NewButton("Починка", func() { m.sendSimpleEvent(state.EventUIClickCleanup) })

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
	statusBox := container.NewVBox(m.loginStatus, retryButton, cleanupButton)
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
	win.SetFixedSize(true)
	m.statusCircle = canvas.NewCircle(theme.DisabledColor())
	m.statusCircle.Resize(fyne.NewSize(14, 14))
	m.mainStatus = widget.NewLabel("Отключено")
	m.spinner = widget.NewProgressBarInfinite()
	m.spinner.Hide()

	m.profileList = widget.NewList(
		func() int { return len(m.profiles) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			label := obj.(*widget.Label)
			if id < 0 || id >= len(m.profiles) {
				label.SetText("-")
				return
			}
			profile := m.profiles[id]
			country := strings.ToUpper(strings.TrimSpace(profile.Country))
			if country == "" {
				country = "?"
			}
			label.SetText(fmt.Sprintf("%s (%s)", profile.Name, country))
		},
	)
	m.profileList.OnSelected = m.handleProfileSelected

	profilesCard := widget.NewCard("Профили", "", container.NewMax(m.profileList))

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
	cleanupBtn := widget.NewButton("Починка", func() { m.sendSimpleEvent(state.EventUIClickCleanup) })
	m.exitBtn = widget.NewButton("Выход", func() { m.sendSimpleEvent(state.EventUIExit) })

	controls := container.NewGridWithColumns(5, m.connectBtn, m.disconnectBtn, m.settingsBtn, cleanupBtn, m.exitBtn)
	mainContent := container.NewBorder(statusBar, controls, nil, nil, profilesCard)
	win.SetContent(container.NewPadded(mainContent))
	win.SetCloseIntercept(func() {
		m.sendSimpleEvent(state.EventTrayHideWindow)
		win.Hide()
		m.mainWinVisible = false
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

func (m *Manager) handleProfileSelected(id widget.ListItemID) {
	if m.suppressProfileSelect {
		return
	}
	if id < 0 || int(id) >= len(m.profiles) {
		return
	}
	profile := m.profiles[id]
	payload := state.SelectionPayload{ID: profile.ID}
	evt := state.Event{Type: state.EventUISelectProfile, Payload: payload, TS: time.Now()}
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
	if m.logger != nil {
		m.logger.Debugf("ui event: %s", t)
	}
	m.dispatchEvent(evt)
}

func (m *Manager) dispatchEvent(evt state.Event) {
	if m.dispatch == nil {
		return
	}
	if m.logger != nil {
		m.logger.Debugf("ui dispatch: %s", evt.Type)
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

func (m *Manager) ensureCleanupDialog() {
	parent := m.activeWindow()
	if m.cleanupDialog != nil && parent == m.cleanupDialogParent {
		return
	}
	if m.cleanupDialog != nil {
		m.cleanupDialog.Hide()
	}
	label := widget.NewLabel("Очистка начата")
	button := widget.NewButton("OK", func() {
		if m.cleanupDialog != nil {
			m.cleanupDialog.Hide()
		}
	})
	button.Disable()
	content := container.NewVBox(label, button)
	dialog := dialog.NewCustomWithoutButtons("Починка", content, parent)
	m.cleanupDialog = dialog
	m.cleanupDialogLabel = label
	m.cleanupDialogButton = button
	m.cleanupDialogParent = parent
}

func (m *Manager) confirmDialog(title, message string) bool {
	if m == nil || m.app == nil {
		return false
	}
	select {
	case <-m.stopCh:
		return false
	default:
	}
	ch := make(chan bool, 1)
	m.callOnUI(func() {
		dialog.ShowConfirm(title, message, func(ok bool) {
			ch <- ok
		}, m.activeWindow())
	})
	return <-ch
}

func (m *Manager) setupTray() {
	if m.app == nil {
		return
	}
	showItem := fyne.NewMenuItem("Показать", func() { m.sendSimpleEvent(state.EventTrayShowWindow) })
	hideItem := fyne.NewMenuItem("Скрыть", func() { m.sendSimpleEvent(state.EventTrayHideWindow) })
	quitItem := fyne.NewMenuItem(lang.L("Quit"), func() { m.sendSimpleEvent(state.EventTrayExit) })
	quitItem.IsQuit = true
	menu := fyne.NewMenu(m.appName, showItem, hideItem, fyne.NewMenuItemSeparator(), quitItem)
	tray := m.trayApp()
	if tray == nil {
		return
	}
	tray.SetSystemTrayMenu(menu)
	tray.SetSystemTrayIcon(theme.FyneLogo())
	systray.SetOnTapped(func() { m.toggleTrayWindow() })
}

func (m *Manager) trayApp() interface {
	SetSystemTrayMenu(*fyne.Menu)
	SetSystemTrayIcon(fyne.Resource)
} {
	if m.app == nil {
		return nil
	}
	tray, ok := m.app.(interface {
		SetSystemTrayMenu(*fyne.Menu)
		SetSystemTrayIcon(fyne.Resource)
	})
	if !ok {
		if m.logger != nil {
			m.logger.Errorf("system tray is not supported by current fyne app")
		}
		return nil
	}
	return tray
}

func (m *Manager) toggleTrayWindow() {
	m.callOnUI(func() {
		if m.loginWinVisible || m.mainWinVisible {
			if m.loginWin != nil {
				m.loginWin.Hide()
				m.loginWinVisible = false
			}
			if m.mainWin != nil {
				m.mainWin.Hide()
				m.mainWinVisible = false
			}
			return
		}
		if m.lastShownLogin && m.loginWin != nil {
			m.loginWin.Show()
			if canvas := m.loginWin.Canvas(); canvas != nil && m.loginEntry != nil {
				canvas.Focus(m.loginEntry)
			}
			m.loginWinVisible = true
			return
		}
		if m.mainWin != nil {
			m.mainWin.Show()
			m.mainWin.RequestFocus()
			m.mainWinVisible = true
		}
	})
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

func findProfileIndex(list []state.Profile, id string) int {
	for i, profile := range list {
		if profile.ID == id {
			return i
		}
	}
	return -1
}
