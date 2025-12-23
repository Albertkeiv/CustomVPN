package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"customvpn/client/internal/controlclient"
	"customvpn/client/internal/routes"
	"customvpn/client/internal/state"
)

const (
	preflightAttempts      = 3
	preflightDelay         = 2 * time.Second
	requestTimeout         = 15 * time.Second
	routeOpTimeout         = 5 * time.Second
	processStopTimeout     = 5 * time.Second
	connectionCheckTimeout = 5 * time.Second
	tunnelDetectTimeout    = 10 * time.Second
	tunnelDetectDelay      = 500 * time.Millisecond
)

func (a *Application) startPreflight(_ *state.AppContext) {
	var lastErr error
	for attempt := 1; attempt <= preflightAttempts; attempt++ {
		if a.isStopping() {
			return
		}
		ctx, cancel := a.requestContext(10 * time.Second)
		err := a.control.CheckHealth(ctx)
		cancel()
		if err == nil {
			a.logger.Infof("preflight succeeded on attempt %d", attempt)
			a.dispatch(state.Event{Type: state.EventSysPreflightSuccess})
			return
		}
		lastErr = err
		a.logger.Errorf("preflight attempt %d/%d failed: %v", attempt, preflightAttempts, err)
		if attempt < preflightAttempts {
			if a.isStopping() {
				return
			}
			time.Sleep(preflightDelay)
		}
	}
	payload := buildPreflightFailurePayload(lastErr)
	a.dispatch(state.Event{Type: state.EventSysPreflightFailure, Payload: payload})
}

func (a *Application) startAuth(_ *state.AppContext, login, password string) {
	if a.isStopping() {
		return
	}
	ctx, cancel := a.requestContext(requestTimeout)
	defer cancel()
	token, err := a.control.Auth(ctx, login, password)
	if err != nil {
		a.logger.Errorf("auth request failed: %v", err)
		payload := buildAuthFailurePayload(err)
		a.dispatch(state.Event{Type: state.EventSysAuthFailure, Payload: payload})
		return
	}
	a.logger.Infof("auth succeeded, token length %d", len(token))
	a.dispatch(state.Event{Type: state.EventSysAuthSuccess, Payload: state.AuthSuccessPayload{Token: token}})
}

func buildAuthFailurePayload(err error) state.ScenarioResultPayload {
	payload := state.ScenarioResultPayload{
		Kind:             state.ErrorKindAuthFailed,
		Message:          "Ошибка авторизации",
		TechnicalMessage: "",
	}
	if err == nil {
		return payload
	}
	payload.TechnicalMessage = err.Error()
	if errors.Is(err, context.DeadlineExceeded) {
		payload.Kind = state.ErrorKindNetworkUnavailable
		payload.Message = "Истекло время ожидания ответа сервера авторизации"
		return payload
	}
	var cErr *controlclient.Error
	if errors.As(err, &cErr) {
		if cErr.Kind != "" {
			payload.Kind = cErr.Kind
		}
		switch cErr.Kind {
		case state.ErrorKindAuthFailed:
			payload.Message = "Неверный логин или пароль"
		case state.ErrorKindNetworkUnavailable:
			payload.Message = "Не удалось подключиться к серверу авторизации"
		default:
			if cErr.Status > 0 {
				payload.Message = fmt.Sprintf("Ошибка авторизации (код %d)", cErr.Status)
			}
		}
	}
	return payload
}

func buildPreflightFailurePayload(err error) state.ScenarioResultPayload {
	payload := state.ScenarioResultPayload{
		Kind:             state.ErrorKindNetworkUnavailable,
		Message:          "Нет связи с управляющим сервером. Повторим через 5 секунд",
		TechnicalMessage: "",
	}
	if err == nil {
		return payload
	}
	payload.TechnicalMessage = err.Error()
	if errors.Is(err, context.DeadlineExceeded) {
		payload.Message = "Истекло время ожидания ответа управляющего сервера"
		return payload
	}
	var cErr *controlclient.Error
	if errors.As(err, &cErr) {
		if cErr.Kind != "" {
			payload.Kind = cErr.Kind
		}
		if cErr.Status > 0 {
			payload.Message = fmt.Sprintf("Управляющий сервер недоступен (код %d)", cErr.Status)
		}
	}
	return payload
}

func buildSyncFailurePayload(err error, fallback string) state.ScenarioResultPayload {
	payload := state.ScenarioResultPayload{
		Kind:             state.ErrorKindSyncFailed,
		Message:          fallback,
		TechnicalMessage: "",
	}
	if err == nil {
		return payload
	}
	payload.TechnicalMessage = err.Error()
	if errors.Is(err, context.DeadlineExceeded) {
		payload.Kind = state.ErrorKindNetworkUnavailable
		payload.Message = "Истекло время ожидания ответа сервера"
		return payload
	}
	var cErr *controlclient.Error
	if errors.As(err, &cErr) {
		if cErr.Kind != "" {
			payload.Kind = cErr.Kind
		}
		if cErr.Status > 0 {
			payload.Message = fmt.Sprintf("%s (код %d)", fallback, cErr.Status)
		}
	}
	return payload
}

func buildPrepareEnvFailurePayload(err error) state.ScenarioResultPayload {
	payload := state.ScenarioResultPayload{
		Kind:             state.ErrorKindRoutingFailed,
		Message:          "Не удалось подготовить маршруты",
		TechnicalMessage: "",
	}
	if err == nil {
		return payload
	}
	payload.TechnicalMessage = err.Error()
	msg := strings.TrimSpace(err.Error())
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "requires elevation") || strings.Contains(lower, "привил") {
		payload.Message = "Недостаточно прав. Запустите приложение от имени администратора"
		return payload
	}
	if strings.Contains(lower, "multiple default gateways") {
		payload.Message = "Обнаружено несколько шлюзов по умолчанию"
		return payload
	}
	if errors.Is(err, context.DeadlineExceeded) {
		payload.Message = "Истекло время ожидания при подготовке маршрутов"
		return payload
	}
	return payload
}

func prepareGatewayErrorMessage(err error) string {
	if err == nil {
		return "РќРµ СѓРґР°Р»РѕСЃСЊ РѕРїСЂРµРґРµР»РёС‚СЊ С€Р»СЋР· РїРѕ СѓРјРѕР»С‡Р°РЅРёСЋ"
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "multiple default gateways") {
		return "РћР±РЅР°СЂСѓР¶РµРЅРѕ РЅРµСЃРєРѕР»СЊРєРѕ С€Р»СЋР·РѕРІ РїРѕ СѓРјРѕР»С‡Р°РЅРёСЋ"
	}
	if strings.Contains(msg, "default gateway not found") {
		return "РќРµ СѓРґР°Р»РѕСЃСЊ РѕРїСЂРµРґРµР»РёС‚СЊ С€Р»СЋР· РїРѕ СѓРјРѕР»С‡Р°РЅРёСЋ"
	}
	return "РќРµ СѓРґР°Р»РѕСЃСЊ РѕРїСЂРµРґРµР»РёС‚СЊ С€Р»СЋР· РїРѕ СѓРјРѕР»С‡Р°РЅРёСЋ"
}

func (a *Application) startSync(appCtx *state.AppContext) {
	if a.isStopping() {
		return
	}
	authToken := strings.TrimSpace(appCtx.AuthToken)
	if authToken == "" {
		a.logger.Errorf("sync requested without auth token")
		payload := buildSyncFailurePayload(errors.New("auth token is empty"), "Не удалось загрузить данные")
		a.dispatch(state.Event{Type: state.EventSysSyncFailure, Payload: payload})
		return
	}
	serversCtx, cancelServers := a.requestContext(requestTimeout)
	servers, err := a.control.SyncServers(serversCtx, authToken)
	cancelServers()
	if err != nil {
		a.logger.Errorf("sync servers failed: %v", err)
		payload := buildSyncFailurePayload(err, "Не удалось загрузить список серверов")
		a.dispatch(state.Event{Type: state.EventSysSyncFailure, Payload: payload})
		return
	}
	routesCtx, cancelRoutes := a.requestContext(requestTimeout)
	routes, err := a.control.SyncRoutes(routesCtx, authToken)
	cancelRoutes()
	if err != nil {
		a.logger.Errorf("sync routes failed: %v", err)
		payload := buildSyncFailurePayload(err, "Не удалось загрузить список маршрутов")
		a.dispatch(state.Event{Type: state.EventSysSyncFailure, Payload: payload})
		return
	}
	payload := state.SyncSuccessPayload{Servers: servers, Routes: routes}
	if err := a.dispatch(state.Event{Type: state.EventSysSyncSuccess, Payload: payload}); err == nil {
		a.logger.Infof("sync completed: %d servers, %d profiles", len(servers), len(routes))
	}
}

func (a *Application) startPrepareEnv(appCtx *state.AppContext) {
	if a.isStopping() {
		return
	}
	_ = appCtx
	payload := state.PrepareEnvSuccessPayload{}
	a.dispatch(state.Event{Type: state.EventSysPrepareEnvSuccess, Payload: payload})
}

func (a *Application) startConnecting(ctx *state.AppContext) {
	if ctx == nil {
		return
	}
	if a.isStopping() {
		return
	}
	artifacts := newConnectArtifacts(a, ctx)
	if err := a.executeConnecting(ctx, artifacts); err != nil {
		artifacts.rollback()
		kind := err.kind
		if kind == "" {
			kind = state.ErrorKindProcessFailed
		}
		message := err.message
		if message == "" {
			message = "РќРµ СѓРґР°Р»РѕСЃСЊ РїРѕРґРєР»СЋС‡РёС‚СЊСЃСЏ"
		}
		if err.err != nil {
			a.logger.Errorf("connecting scenario failed: %v", err.err)
		} else {
			a.logger.Errorf("connecting scenario failed: %s", message)
		}
		payload := state.ScenarioResultPayload{Kind: kind, Message: message}
		a.dispatch(state.Event{Type: state.EventSysConnectingFailure, Payload: payload})
		return
	}
	a.logger.Infof("connecting scenario completed")
	a.dispatch(state.Event{Type: state.EventSysConnectingSuccess})
}

func (a *Application) startDisconnecting(ctx *state.AppContext) {
	if ctx == nil {
		return
	}
	if a.isStopping() {
		return
	}
	if err := a.executeDisconnecting(ctx); err != nil {
		a.logger.Errorf("disconnecting scenario completed with errors: %v", err)
	} else {
		a.logger.Infof("disconnecting scenario completed")
	}
	a.dispatch(state.Event{Type: state.EventSysDisconnectingDone})
}

func (a *Application) launchProcess(name state.ProcessName, binary, logFile string, args []string) (*state.ProcessRecord, error) {
	if a.ctx == nil {
		return nil, fmt.Errorf("app context is not initialized")
	}
	if a.launcher == nil {
		return nil, fmt.Errorf("process launcher is not initialized")
	}
	startRecord := state.ProcessRecord{
		Name:      name,
		Command:   binary,
		Args:      append([]string{}, args...),
		StartedAt: time.Now(),
		Status:    state.ProcessStarting,
	}
	a.ctx.ProcessRegistry.Update(startRecord)
	record, err := a.launcher.Start(name, binary, args, logFile)
	if err != nil {
		exitTime := time.Now()
		startRecord.ExitedAt = &exitTime
		startRecord.Status = state.ProcessFailed
		startRecord.ExitReason = err.Error()
		startRecord.ExitCode = intPtr(-1)
		a.ctx.ProcessRegistry.Update(startRecord)
		return nil, err
	}
	a.ctx.ProcessRegistry.Update(*record)
	return record, nil
}

func (a *Application) stopProcess(name state.ProcessName, timeout time.Duration) {
	if a.launcher == nil {
		return
	}
	if err := a.launcher.Stop(name, timeout); err != nil {
		a.logger.Errorf("stop %s failed: %v", name, err)
	}
}

func (a *Application) requestContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = requestTimeout
	}
	parent := context.Background()
	if a != nil && a.runCtx != nil {
		parent = a.runCtx
	}
	return context.WithTimeout(parent, timeout)
}

func (a *Application) isStopping() bool {
	if a == nil || a.runCtx == nil {
		return false
	}
	select {
	case <-a.runCtx.Done():
		return true
	default:
		return false
	}
}

func (a *Application) resolveControlIPv4() (net.IP, error) {
	if a.controlIP4 != nil {
		return a.controlIP4, nil
	}
	parsed, err := url.Parse(a.cfg.ControlServerURL)
	if err != nil {
		return nil, fmt.Errorf("parse control server url: %w", err)
	}
	host := parsed.Hostname()
	if host == "" {
		return nil, fmt.Errorf("control server host is empty")
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", host, err)
	}
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			a.controlIP4 = v4
			return v4, nil
		}
	}
	return nil, fmt.Errorf("no IPv4 records for %s", host)
}

func (a *Application) executeConnecting(ctx *state.AppContext, artifacts *connectArtifacts) *scenarioError {
	if a.cfg == nil {
		return newScenarioError(state.ErrorKindConfigFailed, "Конфигурация приложения не загружена", fmt.Errorf("config is nil"))
	}
	if a.routes == nil {
		return newScenarioError(state.ErrorKindRoutingFailed, "Маршрутизатор не инициализирован", fmt.Errorf("route manager is nil"))
	}
	gateway, err := routes.DetectDefaultGateway()
	if err != nil {
		return newScenarioError(state.ErrorKindRoutingFailed, prepareGatewayErrorMessage(err), err)
	}
	ctx.DefaultGateway = gateway
	server := ctx.FindServer(ctx.SelectedServerID)
	if server == nil {
		return newScenarioError(state.ErrorKindConfigFailed, "Не удалось найти выбранный сервер", fmt.Errorf("server %s not found", ctx.SelectedServerID))
	}
	if strings.TrimSpace(server.Host) == "" {
		return newScenarioError(state.ErrorKindConfigFailed, "Сервер не содержит адрес", fmt.Errorf("server host is empty"))
	}
	if server.Port <= 0 {
		return newScenarioError(state.ErrorKindConfigFailed, "Сервер не содержит корректный порт", fmt.Errorf("server port %d invalid", server.Port))
	}
	profile := ctx.FindRouteProfile(ctx.SelectedProfileID)
	if profile == nil {
		return newScenarioError(state.ErrorKindConfigFailed, "Не удалось найти профиль маршрутов", fmt.Errorf("profile %s not found", ctx.SelectedProfileID))
	}
	if err := a.addProfileRoutes(ctx, profile.DirectRoutes, state.RouteKindDirect, ctx.DefaultGateway, artifacts); err != nil {
		return err
	}
	if err := a.applyKillSwitch(ctx, artifacts); err != nil {
		return err
	}
	configPath, err := a.writeCoreConfig(server)
	if err != nil {
		return newScenarioError(state.ErrorKindConfigFailed, "Не удалось записать конфигурацию Core", err)
	}
	if err := a.checkCoreConfig(configPath); err != nil {
		return newScenarioError(state.ErrorKindConfigFailed, "Проверка конфигурации Core не прошла", err)
	}
	coreArgs := []string{"run", "-c", configPath}
	if _, err := a.launchProcess(state.ProcessCore, a.cfg.CorePath, a.cfg.CoreLogFile, coreArgs); err != nil {
		return newScenarioError(state.ErrorKindProcessFailed, "Не удалось запустить Core", err)
	}
	artifacts.coreStarted = true
	tunnelGateway, err := a.waitForTunnelGateway(tunnelDetectTimeout)
	if err != nil {
		return newScenarioError(state.ErrorKindRoutingFailed, "Не удалось определить интерфейс туннеля", err)
	}
	if err := a.applyTunnelDNS(ctx, tunnelGateway, artifacts); err != nil {
		return err
	}
	if err := a.addProfileRoutes(ctx, profile.TunnelRoutes, state.RouteKindTunnel, tunnelGateway, artifacts); err != nil {
		return err
	}
	return nil
}

func (a *Application) executeDisconnecting(ctx *state.AppContext) error {
	a.stopProcess(state.ProcessCore, processStopTimeout)
	if ctx != nil {
		a.removeKillSwitch(ctx, nil)
	}
	if a.routes == nil || ctx == nil {
		return nil
	}
	routes := ctx.RoutesRegistry.ListByKinds(state.RouteKindDirect, state.RouteKindTunnel)
	var errs []string
	for _, record := range routes {
		if err := a.removeRouteRecord(ctx, record); err != nil {
			a.logger.Errorf("remove route %s failed: %v", record.Destination, err)
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf(strings.Join(errs, "; "))
	}
	return nil
}

func (a *Application) writeCoreConfig(server *state.Server) (string, error) {
	if server == nil {
		return "", fmt.Errorf("server is nil")
	}
	if len(server.CoreConfigRaw) == 0 {
		return "", fmt.Errorf("core config for server %s is empty", server.ID)
	}
	configDir := filepath.Join(a.cfg.AppDir, "core_config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return "", fmt.Errorf("create core_config dir: %w", err)
	}
	fileName := fmt.Sprintf("%s.json", sanitizeFileName(server.Name, server.ID))
	fullPath := filepath.Join(configDir, fileName)
	if err := os.WriteFile(fullPath, server.CoreConfigRaw, 0o600); err != nil {
		return "", fmt.Errorf("write core config: %w", err)
	}
	server.CoreConfigFilePath = fullPath
	return fullPath, nil
}

func (a *Application) checkCoreConfig(path string) error {
	if a.cfg == nil || strings.TrimSpace(a.cfg.CorePath) == "" {
		return fmt.Errorf("core path is not configured")
	}
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("core config path is empty")
	}
	cmd := exec.Command(a.cfg.CorePath, "check", "-c", path)
	cmd.Dir = filepath.Dir(a.cfg.CorePath)
	applyCommandAttributes(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		out := strings.TrimSpace(string(output))
		if out != "" {
			return fmt.Errorf("%w: %s", err, out)
		}
		return err
	}
	return nil
}
func (a *Application) removeRouteRecord(ctx *state.AppContext, record state.RouteRecord) error {
	if a.routes == nil {
		return fmt.Errorf("route manager is nil")
	}
	routeCtx, cancel := a.requestContext(routeOpTimeout)
	defer cancel()
	if err := a.routes.RemoveRoute(routeCtx, record); err != nil {
		return err
	}
	ctx.RoutesRegistry.Remove(record.ID)
	return nil
}

func (a *Application) addProfileRoutes(ctx *state.AppContext, cidrs []string, kind state.RouteKind, gateway *state.GatewayInfo, artifacts *connectArtifacts) *scenarioError {
	if a.routes == nil {
		return newScenarioError(state.ErrorKindRoutingFailed, "Маршрутизатор не инициализирован", fmt.Errorf("route manager is nil"))
	}
	if gateway == nil || strings.TrimSpace(gateway.IP) == "" {
		return newScenarioError(state.ErrorKindRoutingFailed, "Маршрутный шлюз не задан", fmt.Errorf("route gateway is nil"))
	}
	for _, cidr := range cidrs {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		routeCtx, cancel := a.requestContext(routeOpTimeout)
		record, err := a.routes.AddCIDRRoute(routeCtx, cidr, gateway, kind)
		cancel()
		if err != nil {
			return newScenarioError(state.ErrorKindRoutingFailed, fmt.Sprintf("Не удалось добавить маршрут %s", cidr), err)
		}
		ctx.RoutesRegistry.Upsert(record)
		if artifacts != nil {
			artifacts.addRoute(record)
		}
	}
	return nil
}

func (a *Application) applyTunnelDNS(ctx *state.AppContext, gateway *state.GatewayInfo, artifacts *connectArtifacts) *scenarioError {
	if a.dns == nil {
		return newScenarioError(state.ErrorKindRoutingFailed, "DNS менеджер не инициализирован", fmt.Errorf("dns manager is nil"))
	}
	if gateway == nil || strings.TrimSpace(gateway.InterfaceName) == "" {
		return newScenarioError(state.ErrorKindRoutingFailed, "Не удалось определить интерфейс туннеля", fmt.Errorf("tunnel interface name is empty"))
	}
	dnsCtx, cancel := a.requestContext(routeOpTimeout)
	defer cancel()
	if err := a.dns.SetInterfaceDNS(dnsCtx, gateway.InterfaceName, []string{"100.64.127.2"}); err != nil {
		return newScenarioError(state.ErrorKindRoutingFailed, "Не удалось настроить DNS туннеля", err)
	}
	if a.logger != nil {
		a.logger.Infof("tunnel DNS set: interface=%s servers=%v", gateway.InterfaceName, []string{"100.64.127.2"})
	}
	return nil
}

func (a *Application) applyKillSwitch(ctx *state.AppContext, artifacts *connectArtifacts) *scenarioError {
	if a.firewall == nil {
		return newScenarioError(state.ErrorKindRoutingFailed, "Kill Switch не инициализирован", fmt.Errorf("firewall manager is nil"))
	}
	if ctx == nil || ctx.DefaultGateway == nil {
		return newScenarioError(state.ErrorKindRoutingFailed, "Kill Switch не может определить основной интерфейс", fmt.Errorf("default gateway is nil"))
	}
	if strings.TrimSpace(ctx.DefaultGateway.InterfaceName) == "" {
		return newScenarioError(state.ErrorKindRoutingFailed, "Kill Switch не может определить основной интерфейс", fmt.Errorf("default gateway interface name is empty"))
	}
	firewallCtx, cancel := a.requestContext(routeOpTimeout)
	defer cancel()
	rules, err := a.firewall.BlockDNSOnInterface(firewallCtx, ctx.DefaultGateway.InterfaceName)
	if err != nil {
		return newScenarioError(state.ErrorKindRoutingFailed, "Не удалось применить Kill Switch", err)
	}
	if a.logger != nil {
		a.logger.Infof("kill switch enabled: interface=%s rules=%v", ctx.DefaultGateway.InterfaceName, rules)
	}
	ctx.KillSwitchRules = append([]string{}, rules...)
	if artifacts != nil {
		artifacts.killSwitchRules = append(artifacts.killSwitchRules, rules...)
	}
	return nil
}

func (a *Application) removeKillSwitch(ctx *state.AppContext, rules []string) {
	if a.firewall == nil {
		return
	}
	if len(rules) == 0 && ctx != nil {
		rules = ctx.KillSwitchRules
	}
	if len(rules) == 0 {
		return
	}
	firewallCtx, cancel := a.requestContext(routeOpTimeout)
	defer cancel()
	if err := a.firewall.RemoveRules(firewallCtx, rules); err != nil {
		if a.logger != nil {
			a.logger.Errorf("kill switch cleanup failed: %v", err)
		}
		return
	}
	if a.logger != nil {
		a.logger.Infof("kill switch disabled: rules=%v", rules)
	}
	if ctx != nil {
		ctx.KillSwitchRules = nil
	}
}

func tunnelGatewayInfo() (*state.GatewayInfo, error) {
	ip := net.ParseIP("100.64.127.1")
	if ip == nil {
		return nil, fmt.Errorf("invalid tunnel gateway ip")
	}
	return routes.DetectGatewayForIP(ip)
}

func (a *Application) waitForTunnelGateway(timeout time.Duration) (*state.GatewayInfo, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for attempt := 1; ; attempt++ {
		if a.isStopping() {
			return nil, fmt.Errorf("tunnel detection canceled")
		}
		gateway, err := tunnelGatewayInfo()
		if err == nil {
			if attempt > 1 && a.logger != nil {
				a.logger.Infof("tunnel interface detected after %d attempts", attempt)
			}
			return gateway, nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			return nil, lastErr
		}
		time.Sleep(tunnelDetectDelay)
	}
}

func sanitizeFileName(name string, fallback string) string {
	base := strings.TrimSpace(name)
	if base == "" {
		base = strings.TrimSpace(fallback)
	}
	if base == "" {
		base = "core-config"
	}
	var b strings.Builder
	for _, r := range base {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		b.WriteRune('_')
	}
	return b.String()
}

type scenarioError struct {
	kind    state.ErrorKind
	message string
	err     error
}

func newScenarioError(kind state.ErrorKind, message string, err error) *scenarioError {
	return &scenarioError{kind: kind, message: message, err: err}
}

type connectArtifacts struct {
	app             *Application
	ctx             *state.AppContext
	routes          []state.RouteRecord
	coreStarted     bool
	killSwitchRules []string
}

func newConnectArtifacts(app *Application, ctx *state.AppContext) *connectArtifacts {
	return &connectArtifacts{app: app, ctx: ctx}
}

func (c *connectArtifacts) addRoute(record state.RouteRecord) {
	c.routes = append(c.routes, record)
}

func (c *connectArtifacts) rollback() {
	if c == nil {
		return
	}
	if c.coreStarted {
		c.app.stopProcess(state.ProcessCore, processStopTimeout)
	}
	if len(c.killSwitchRules) > 0 {
		c.app.removeKillSwitch(c.ctx, c.killSwitchRules)
	}
	for i := len(c.routes) - 1; i >= 0; i-- {
		if err := c.app.removeRouteRecord(c.ctx, c.routes[i]); err != nil {
			c.app.logger.Errorf("rollback remove route %s failed: %v", c.routes[i].Destination, err)
		}
	}
}








