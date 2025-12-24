package state

import (
	"encoding/json"
	"sync"
	"time"

	"customvpn/client/internal/config"
)

// ErrorKind описывает тип ошибки, отображаемой пользователю и используемой для логики состояния.
type ErrorKind string

const (
	ErrorKindNetworkUnavailable ErrorKind = "NetworkUnavailable"
	ErrorKindAuthFailed         ErrorKind = "AuthFailed"
	ErrorKindSyncFailed         ErrorKind = "SyncFailed"
	ErrorKindRoutingFailed      ErrorKind = "RoutingFailed"
	ErrorKindProcessFailed      ErrorKind = "ProcessFailed"
	ErrorKindConfigFailed       ErrorKind = "ConfigFailed"
	ErrorKindUnknown            ErrorKind = "Unknown"
)

// Profile описывает прокси-сервер, полученный от Control-сервера и используемый в приложении.
type Profile struct {
	ID                 string          `json:"id"`
	Name               string          `json:"name"`
	Country            string          `json:"country"`
	Host               string          `json:"host"`
	Port               int             `json:"port"`
	CoreConfigRaw      json.RawMessage `json:"core_config"`
	DirectRoutes       []string        `json:"direct_routes"`
	TunnelRoutes       []string        `json:"tunnel_routes"`
	KillSwitchEnabled  bool            `json:"kill_switch"`
	CoreConfigFilePath string          `json:"-"`
}

// GatewayInfo описывает маршрут по умолчанию Windows.
type GatewayInfo struct {
	IP             string
	InterfaceIndex int
	InterfaceName  string
	Metric         int
}

// RouteKind классифицирует маршруты в RoutesRegistry.
type RouteKind string

const (
	RouteKindService RouteKind = "Service"
	RouteKindDirect  RouteKind = "Direct"
	RouteKindTunnel  RouteKind = "Tunnel"
)

// RouteRecord описывает одну запись маршрута.
type RouteRecord struct {
	ID             string
	Destination    string
	Gateway        string
	InterfaceIndex int
	Metric         int
	Kind           RouteKind
	CreatedAt      time.Time
	Active         bool
}

// RoutesRegistry хранит добавленные маршруты.
type RoutesRegistry struct {
	mu     sync.RWMutex
	Routes map[string]RouteRecord
}

// NewRoutesRegistry создаёт пустой реестр маршрутов.
func NewRoutesRegistry() RoutesRegistry {
	return RoutesRegistry{Routes: make(map[string]RouteRecord)}
}

// Upsert обновляет или добавляет запись маршрута.
func (r *RoutesRegistry) Upsert(record RouteRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now()
	}
	r.Routes[record.ID] = record
}

// Remove удаляет запись маршрута по ID.
func (r *RoutesRegistry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.Routes, id)
}

// ListByKinds возвращает копию записей по указанным типам маршрутов.
// Если список kinds пуст, возвращаются все маршруты.
func (r *RoutesRegistry) ListByKinds(kinds ...RouteKind) []RouteRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(kinds) == 0 {
		all := make([]RouteRecord, 0, len(r.Routes))
		for _, record := range r.Routes {
			all = append(all, record)
		}
		return all
	}
	set := make(map[RouteKind]struct{}, len(kinds))
	for _, kind := range kinds {
		set[kind] = struct{}{}
	}
	filtered := make([]RouteRecord, 0, len(r.Routes))
	for _, record := range r.Routes {
		if _, ok := set[record.Kind]; ok {
			filtered = append(filtered, record)
		}
	}
	return filtered
}

// ProcessName идентифицирует процесс Core.
type ProcessName string

const (
	ProcessCore ProcessName = "Core"
)

// ProcessStatus описывает статус отслеживаемого процесса.
type ProcessStatus string

const (
	ProcessStarting ProcessStatus = "Starting"
	ProcessRunning  ProcessStatus = "Running"
	ProcessExited   ProcessStatus = "Exited"
	ProcessFailed   ProcessStatus = "Failed"
)

// ProcessRecord хранит сведения о дочернем процессе.
type ProcessRecord struct {
	Name       ProcessName
	Command    string
	Args       []string
	PID        int
	StartedAt  time.Time
	ExitedAt   *time.Time
	Status     ProcessStatus
	ExitCode   *int
	ExitReason string
}

// ProcessRegistry хранит статусы процессов Core.
type ProcessRegistry struct {
	mu        sync.RWMutex
	Processes map[ProcessName]ProcessRecord
}

// NewProcessRegistry создаёт пустой реестр процессов.
func NewProcessRegistry() ProcessRegistry {
	return ProcessRegistry{Processes: make(map[ProcessName]ProcessRecord)}
}

// Update заменяет запись по имени процесса.
func (r *ProcessRegistry) Update(record ProcessRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Processes[record.Name] = record
}

// Get возвращает запись процесса, если она существует.
func (r *ProcessRegistry) Get(name ProcessName) (ProcessRecord, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	record, ok := r.Processes[name]
	return record, ok
}

// ErrorInfo описывает ошибку для UI и логов.
type ErrorInfo struct {
	Kind             ErrorKind
	UserMessage      string
	TechnicalMessage string
	OccurredAt       time.Time
}

// UIState хранит минимально необходимую информацию для управления UI.
type UIState struct {
	IsLoginVisible      bool
	IsMainVisible       bool
	IsConnecting        bool
	IsConnected         bool
	SelectedProfileID   string
	StatusText          string
	LoginInput          string
	PasswordInput       string
	CanLogin            bool
	AllowPreflightRetry bool
}

// AppContext содержит всё состояние приложения.
type AppContext struct {
	Config            *config.Config
	AuthToken         string
	Profiles          []Profile
	SelectedProfileID string
	DefaultGateway    *GatewayInfo
	KillSwitchRules   []string
	RoutesRegistry    RoutesRegistry
	ProcessRegistry   ProcessRegistry
	LastError         *ErrorInfo
	UI                UIState
	State             State
}

// NewAppContext создаёт AppContext с инициализированными реестрами.
func NewAppContext(cfg *config.Config) *AppContext {
	return &AppContext{
		Config:          cfg,
		RoutesRegistry:  NewRoutesRegistry(),
		ProcessRegistry: NewProcessRegistry(),
		State:           StateAppStarting,
	}
}

func (ctx *AppContext) FindProfile(id string) *Profile {
	for i := range ctx.Profiles {
		if ctx.Profiles[i].ID == id {
			return &ctx.Profiles[i]
		}
	}
	return nil
}

