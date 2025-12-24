package controlclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"customvpn/client/internal/logging"
	"customvpn/client/internal/state"
)

// Client инкапсулирует HTTP-взаимодействия с Control-сервером.
type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
	logger     *logging.Logger
}

// Options позволяет переопределить зависимости клиента.
type Options struct {
	HTTPClient *http.Client
	Logger     *logging.Logger
}

const (
	defaultTimeout = 15 * time.Second
)

// New создаёт новый клиент Control-сервера.
func New(baseURL string, opts Options) (*Client, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("baseURL is empty")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse baseURL: %w", err)
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{baseURL: parsed, httpClient: client, logger: opts.Logger}, nil
}

// Error описывает проблему при запросах к Control-серверу.
type Error struct {
	Op     string
	Kind   state.ErrorKind
	Status int
	Err    error
}

func (e *Error) Error() string {
	if e == nil {
		return "control client error"
	}
	return fmt.Sprintf("%s: %s", e.Op, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

// CheckHealth выполняет GET /health и ожидает строку "OK".
func (c *Client) CheckHealth(ctx context.Context) error {
	const op = "CheckHealth"
	resp, err := c.do(ctx, http.MethodGet, "/health", "", nil)
	if err != nil {
		return wrapError(op, state.ErrorKindNetworkUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &Error{Op: op, Kind: state.ErrorKindNetworkUnavailable, Status: resp.StatusCode, Err: fmt.Errorf("unexpected status %d", resp.StatusCode)}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return wrapError(op, state.ErrorKindNetworkUnavailable, err)
	}
	text := strings.TrimSpace(string(body))
	if text == "OK" {
		return nil
	}
	if unquoted, err := strconv.Unquote(text); err == nil && strings.TrimSpace(unquoted) == "OK" {
		return nil
	}
	return &Error{Op: op, Kind: state.ErrorKindNetworkUnavailable, Status: http.StatusOK, Err: fmt.Errorf("unexpected body %q", string(body))}
}

// Auth вызывает /auth и возвращает authToken.
func (c *Client) Auth(ctx context.Context, login, password string) (string, error) {
	const op = "Auth"
	payload := AuthRequest{Login: login, Password: password}
	resp, err := c.doJSON(ctx, http.MethodPost, "/auth", "", payload)
	if err != nil {
		return "", wrapError(op, state.ErrorKindNetworkUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", &Error{Op: op, Kind: state.ErrorKindAuthFailed, Status: resp.StatusCode, Err: errors.New("auth failed")}
	}
	if resp.StatusCode != http.StatusOK {
		return "", &Error{Op: op, Kind: state.ErrorKindUnknown, Status: resp.StatusCode, Err: fmt.Errorf("unexpected status %d", resp.StatusCode)}
	}
	var body AuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", wrapError(op, state.ErrorKindUnknown, err)
	}
	if strings.TrimSpace(body.AuthToken) == "" {
		return "", &Error{Op: op, Kind: state.ErrorKindUnknown, Status: http.StatusOK, Err: errors.New("empty auth token")}
	}
	return body.AuthToken, nil
}

// SyncProfileList вызывает /sync/profiles.
func (c *Client) SyncProfileList(ctx context.Context, authToken string) ([]state.Profile, error) {
	const op = "SyncProfileList"
	resp, err := c.do(ctx, http.MethodGet, "/sync/profiles", authToken, nil)
	if err != nil {
		return nil, wrapError(op, state.ErrorKindNetworkUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &Error{Op: op, Kind: state.ErrorKindSyncFailed, Status: resp.StatusCode, Err: fmt.Errorf("unexpected status %d", resp.StatusCode)}
	}
	var payload []ProfileSummaryDTO
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, wrapError(op, state.ErrorKindSyncFailed, err)
	}
	profiles := make([]state.Profile, 0, len(payload))
	for _, dto := range payload {
		profile, err := dto.Validate()
		if err != nil {
			return nil, wrapError(op, state.ErrorKindSyncFailed, err)
		}
		profiles = append(profiles, profile)
	}
	return profiles, nil
}

// SyncProfile вызывает /profiles/{id}.
func (c *Client) SyncProfile(ctx context.Context, authToken string, id string) (state.Profile, error) {
	const op = "SyncProfile"
	id = strings.TrimSpace(id)
	if id == "" {
		return state.Profile{}, wrapError(op, state.ErrorKindSyncFailed, errors.New("profile id is empty"))
	}
	resp, err := c.do(ctx, http.MethodGet, "/profiles/"+url.PathEscape(id), authToken, nil)
	if err != nil {
		return state.Profile{}, wrapError(op, state.ErrorKindNetworkUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return state.Profile{}, &Error{Op: op, Kind: state.ErrorKindSyncFailed, Status: resp.StatusCode, Err: fmt.Errorf("unexpected status %d", resp.StatusCode)}
	}
	var payload ProfileDTO
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return state.Profile{}, wrapError(op, state.ErrorKindSyncFailed, err)
	}
	profile, err := payload.Validate()
	if err != nil {
		return state.Profile{}, wrapError(op, state.ErrorKindSyncFailed, err)
	}
	return profile, nil
}

func (c *Client) do(ctx context.Context, method, path, authToken string, body io.Reader) (*http.Response, error) {
	rel, err := url.Parse(path)
	if err != nil {
		return nil, err
	}
	full := c.baseURL.ResolveReference(rel)
	req, err := http.NewRequestWithContext(ctx, method, full.String(), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) doJSON(ctx context.Context, method, path, authToken string, payload any) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		buf := &bytes.Buffer{}
		if err := json.NewEncoder(buf).Encode(payload); err != nil {
			return nil, err
		}
		body = buf
	}
	return c.do(ctx, method, path, authToken, body)
}

func wrapError(op string, kind state.ErrorKind, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Op: op, Kind: kind, Err: err}
}
