## Модели данных и контракты API (MVP)

Этот документ описывает формальные структуры данных и форматы обмена с Control-сервером для реализации MVP.

Документ дополняет init-specs.md и state-specs.md и отражает только минимально необходимый набор полей.

---

## 1. Конфигурация приложения (Config)

Файл конфигурации `config.yaml` расположен в каталоге приложения (рядом с исполняемым файлом). Формат YAML.

### 1.1. Структура Config

Поля:

- `control_server_url: string` — базовый URL Control-сервера (например, `https://control.example.com`).
- `core_path: string` — путь к бинарнику Core (по умолчанию `<app_dir>/<core-name>`).
- `log_level: string` — уровень логирования: одно из значений `debug`, `info`, `error`.
- `log_file: string` — путь к основному лог-файлу приложения.

Внутренние вычисляемые поля (не в YAML):

- `appDir: string` — каталог приложения, используется для построения путей `core_config/…`, логов Core и т.п.

### 1.2. Пример config.yaml

```yaml
control_server_url: "https://control.example.com"
core_path: "./sing-box.exe"
log_level: "info"
log_file: "./logs/app.log"
```

При отсутствии файла или ошибке парсинга YAML генерируется ошибка ConfigFailed.

---

## 2. Контракты Control-сервера (HTTP API)

Все запросы/ответы в MVP используют формат JSON. Базовый URL берётся из `Config.control_server_url`.

### 2.1. /health

- Метод: `GET /health`
- Запрос: без тела.
- Успешный ответ:
  - HTTP-код: `200`
  - Тело: строка `"OK"` (без обёртки в объект).
- Любой другой код или содержимое тела считается ошибкой.

### 2.2. /auth

- Метод: `POST /auth`
- URL: `Config.control_server_url + "/auth"`
- Тело запроса (JSON):

```json
{
  "login": "user@example.com",
  "password": "secret"
}
```

- Успешный ответ:
  - HTTP-код: `200`
  - Тело (JSON):

```json
{
  "authToken": "<opaque-token>"
}
```

- Неуспешная авторизация:
  - HTTP-код: 4xx (например, 401);
  - и/или тело: строка `"Auth Failed"`.
  - Маппинг в приложении: SYS_РезультатAuth(ошибка) → Error(AuthFailed).

### 2.3. /sync/servers

- Метод: `GET /sync/servers`
- Авторизация: использование `authToken` (заголовок или параметр — определяется сервером; клиент должен поддерживать выбранный способ).
- Успешный ответ (JSON): массив объектов `ServerDTO`.

#### ServerDTO (формат ответа)

```json
{
  "id": "server-1",
  "name": "Frankfurt #1",
  "country": "DE",
  "host": "fr1.example.com",
  "port": 443,
  "core_config": { "...": "..." }
}
```

Поля:

- `id: string` — стабильный идентификатор сервера.
- `name: string` — отображаемое имя (показывается в UI).
- `country: string` — код страны (например, `DE`), используется для текста/иконки.
- `host: string` — адрес прокси (FQDN или IP).
- `port: number` — порт прокси.
- `core_config: object` — произвольный JSON для Core; клиент не интерпретирует, а просто сохраняет в файл.

#### Внутренний Server (модель приложения)

- `ID: string`
- `Name: string`
- `Country: string`
- `Host: string`
- `Port: int`
- `CoreConfigRaw: map<string, any> | string` — исходный JSON как пришёл от сервера.
- `CoreConfigFilePath: string` — путь к файлу конфигурации Core для этого сервера, например `<app_dir>/core_config/<server-name>.json`.

Любая ошибка формата/валидации любого элемента массива `/sync/servers` приводит к целиковому провалу Sync и Error(SyncFailed).

### 2.4. /sync/routes

- Метод: `GET /sync/routes`
- Авторизация: аналогично `/sync/servers`.
- Успешный ответ (JSON): массив объектов `RouteProfileDTO`.

#### RouteProfileDTO (формат ответа)

```json
{
  "id": "profile-1",
  "name": "Default",
  "direct_routes": [
    "10.0.0.0/8",
    "192.168.0.0/16"
  ],
  "tunnel_routes": [
    "0.0.0.0/0"
  ]
}
```

Поля:

- `id: string` — идентификатор профиля.
- `name: string` — отображаемое имя профиля (показывается в UI).
- `direct_routes: string[]` — список IPv4-подсетей в формате CIDR (`A.B.C.D/M`), которые идут напрямую (мимо туннеля).
- `tunnel_routes: string[]` — список IPv4-подсетей/правил, которые должны идти через туннель (может быть `"0.0.0.0/0"`).

#### Внутренний RouteProfile (модель приложения)

- `ID: string`
- `Name: string`
- `DirectRoutes: []string` — CIDR-строки.
- `TunnelRoutes: []string` — CIDR-строки.

Любая ошибка формата/валидации любого элемента массива `/sync/routes` также валит всю операцию Sync.

---

## 3. GatewayInfo и маршруты

### 3.1. GatewayInfo

`GatewayInfo` описывает маршрут по умолчанию, найденный в PreparingEnvironment.

Поля:

- `IP: string` — IPv4-адрес шлюза.
- `InterfaceIndex: int` — индекс интерфейса Windows, по которому идёт default route (0.0.0.0/0).
- `Metric: int` — метрика default route.

### 3.2. RouteRecord и RoutesRegistry

#### RouteKind

Перечисление видов маршрутов:

- `Service` — служебный маршрут до Control-сервера.
- `Direct` — прямой маршрут до прокси или подсетей, идущих мимо туннеля.
- `Tunnel` — маршруты, специфичные для туннельной схемы.

#### RouteRecord

Поля:

- `ID: string` — внутренний идентификатор записи (например, UUID или детерминированный ключ).
- `Destination: string` — сеть/хост в формате, удобном для `route add` (например, CIDR-строка `A.B.C.D/M`).
- `Gateway: string` — IP шлюза.
- `InterfaceIndex: int` — индекс интерфейса.
- `Metric: int` — метрика маршрута.
- `Kind: RouteKind` — тип маршрута (Service/Direct/Tunnel).
- `CreatedAt: time` — время создания маршрута (для логов/отладки).
- `Active: bool` — маршрут считается актуальным/действующим.

#### RoutesRegistry

Поля:

- `Routes: map<string, RouteRecord>` — ключ по `RouteRecord.ID`.

Semantics:

- Все маршруты, добавленные приложением, должны попадать в `RoutesRegistry`.
- При Disconnecting и Exiting маршруты удаляются на основе данных из `RoutesRegistry`.

---

## 4. ProcessRegistry и процесс Core

### 4.1. ProcessName и ProcessStatus

- `ProcessName: { Core }` — тип процесса.
- `ProcessStatus: { Starting, Running, Exited, Failed }` — состояние.

### 4.2. ProcessRecord

Поля:

- `Name: ProcessName`
- `Command: string` — полный путь к бинарнику.
- `Args: []string` — аргументы запуска (в MVP могут быть частично-заглушками, но структура фиксируется).
- `PID: int` — PID процесса (0, если не запущен).
- `StartedAt: time`
- `ExitedAt: time | null`
- `Status: ProcessStatus`
- `ExitCode: int | null`
- `ExitReason: string | null` — краткая причина (для логов).

### 4.3. ProcessRegistry

Поля:

- `Processes: map<ProcessName, ProcessRecord>`

Semantics:

- Для Core в любой момент времени должна существовать не более одной активной записи.
- Все изменения статуса процессов (запуск, падение, успешное завершение) отражаются в `ProcessRegistry` и логируются.

---

## 5. ErrorInfo и ошибки

### 5.1. ErrorKind

Используются значения из state-specs.md:

- `NetworkUnavailable`
- `AuthFailed`
- `SyncFailed`
- `RoutingFailed`
- `ProcessFailed`
- `ConfigFailed`
- `Unknown`

### 5.2. ErrorInfo

Поля:

- `Kind: ErrorKind` — тип ошибки.
- `UserMessage: string` — краткое сообщение для UI (текст модального окна).
- `TechnicalMessage: string` — подробное сообщение для логов (стек, коды ошибок, URL запросов и т.п.).
- `OccurredAt: time` — время возникновения ошибки.

Связь с UI описана в разделе 10.6 state-specs.md.

---

## 6. UIState и AppContext (срез для MVP)

### 6.1. UIState

Минимальный набор полей для управления состоянием UI:

- `IsLoginVisible: bool`
- `IsMainVisible: bool`
- `IsConnecting: bool`
- `IsConnected: bool`
- `SelectedServerID: string | null`
- `SelectedProfileID: string | null`
- `StatusText: string` — текст статуса ("Подключено", "Отключено", "Ошибка авторизации" и т.п.).

### 6.2. AppContext (уточнение)

Расширение описания из state-specs.md:

- `Config: Config`
- `AuthToken: string | null`
- `ServersList: []Server`
- `RoutesProfiles: []RouteProfile`
- `SelectedServerID: string | null`
- `SelectedProfileID: string | null`
- `DefaultGateway: GatewayInfo | null`
- `RoutesRegistry: RoutesRegistry`
- `ProcessRegistry: ProcessRegistry`
- `LastError: ErrorInfo | null`
- `UI: UIState`

Эти структуры задают основу для реализации MVP без необходимости додумывать поля в коде.
