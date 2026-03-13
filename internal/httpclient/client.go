package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sbekti/internctl/internal/api"
	"github.com/sbekti/internctl/internal/session"
)

var ErrUnauthorized = errors.New("unauthorized")
var ErrForbidden = errors.New("forbidden")

type Client struct {
	baseURL       string
	anonymous     *api.ClientWithResponses
	authenticated *api.ClientWithResponses
}

type AuthTransport struct {
	base      http.RoundTripper
	baseURL   string
	profile   string
	backend   session.Backend
	sessions  *session.Manager
	refreshMu sync.Mutex
}

type APIError struct {
	StatusCode int
	Code       string
	Message    string
}

type ClientAuthError struct {
	Code        string
	Description string
	StatusCode  int
}

func (e APIError) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("request failed with status %d", e.StatusCode)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e ClientAuthError) Error() string {
	if e.Description == "" {
		return e.Code
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Description)
}

func New(baseURL, profile string, backend session.Backend, sessions *session.Manager) (*Client, error) {
	trimmedBaseURL := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmedBaseURL == "" {
		return nil, errors.New("server URL must not be empty")
	}

	anonymous, err := api.NewClientWithResponses(trimmedBaseURL)
	if err != nil {
		return nil, fmt.Errorf("create anonymous API client: %w", err)
	}

	authTransport := &AuthTransport{
		base:     http.DefaultTransport,
		baseURL:  trimmedBaseURL,
		profile:  profile,
		backend:  backend,
		sessions: sessions,
	}

	authenticated, err := api.NewClientWithResponses(
		trimmedBaseURL,
		api.WithHTTPClient(&http.Client{Transport: authTransport}),
	)
	if err != nil {
		return nil, fmt.Errorf("create authenticated API client: %w", err)
	}

	return &Client{
		baseURL:       trimmedBaseURL,
		anonymous:     anonymous,
		authenticated: authenticated,
	}, nil
}

func (c *Client) CreateDeviceCode(ctx context.Context, clientName string) (*api.DeviceCode, error) {
	req := api.DeviceCodeCreateRequest{}
	if strings.TrimSpace(clientName) != "" {
		name := strings.TrimSpace(clientName)
		req.ClientName = &name
	}

	resp, err := c.anonymous.CreateDeviceCodeWithResponse(ctx, req)
	if err != nil {
		return nil, err
	}

	switch resp.StatusCode() {
	case http.StatusCreated:
		return resp.JSON201, nil
	case http.StatusTooManyRequests:
		return nil, newAPIError(resp.StatusCode(), resp.JSON429)
	default:
		return nil, unexpectedStatus("create device code", resp.StatusCode(), resp.Body)
	}
}

func (c *Client) ExchangeDeviceCode(ctx context.Context, deviceCode string) (*api.TokenResponse, error) {
	resp, err := c.anonymous.ExchangeDeviceCodeWithResponse(ctx, api.DeviceCodeTokenRequest{
		DeviceCode: deviceCode,
	})
	if err != nil {
		return nil, err
	}

	switch resp.StatusCode() {
	case http.StatusOK:
		return resp.JSON200, nil
	case http.StatusBadRequest:
		return nil, ClientAuthError{
			Code:        string(resp.JSON400.Error),
			Description: resp.JSON400.ErrorDescription,
			StatusCode:  resp.StatusCode(),
		}
	case http.StatusPreconditionRequired:
		return nil, ClientAuthError{
			Code:        string(resp.JSON428.Error),
			Description: resp.JSON428.ErrorDescription,
			StatusCode:  resp.StatusCode(),
		}
	default:
		return nil, unexpectedStatus("exchange device code", resp.StatusCode(), resp.Body)
	}
}

func (c *Client) Logout(ctx context.Context, refreshToken string) error {
	resp, err := c.anonymous.LogoutSessionWithResponse(ctx, api.LogoutRequest{
		RefreshToken: refreshToken,
	})
	if err != nil {
		return err
	}

	switch resp.StatusCode() {
	case http.StatusNoContent:
		return nil
	case http.StatusBadRequest:
		return newAPIError(resp.StatusCode(), resp.JSON400)
	default:
		return unexpectedStatus("logout", resp.StatusCode(), resp.Body)
	}
}

func (c *Client) GetProfile(ctx context.Context) (*api.Profile, error) {
	resp, err := c.authenticated.GetProfileWithResponse(ctx)
	if err != nil {
		return nil, err
	}

	switch resp.StatusCode() {
	case http.StatusOK:
		return resp.JSON200, nil
	case http.StatusUnauthorized:
		return nil, ErrUnauthorized
	default:
		return nil, unexpectedStatus("get profile", resp.StatusCode(), resp.Body)
	}
}

func (c *Client) ListVlans(ctx context.Context) ([]api.Vlan, error) {
	resp, err := c.authenticated.ListVlansWithResponse(ctx)
	if err != nil {
		return nil, err
	}

	switch resp.StatusCode() {
	case http.StatusOK:
		return resp.JSON200.Items, nil
	case http.StatusUnauthorized:
		return nil, ErrUnauthorized
	case http.StatusForbidden:
		return nil, ErrForbidden
	default:
		return nil, unexpectedStatus("list vlans", resp.StatusCode(), resp.Body)
	}
}

func (c *Client) ListNetworkDevices(ctx context.Context) ([]api.NetworkDevice, error) {
	resp, err := c.authenticated.ListNetworkDevicesWithResponse(ctx)
	if err != nil {
		return nil, err
	}

	switch resp.StatusCode() {
	case http.StatusOK:
		return resp.JSON200.Items, nil
	case http.StatusUnauthorized:
		return nil, ErrUnauthorized
	case http.StatusForbidden:
		return nil, ErrForbidden
	default:
		return nil, unexpectedStatus("list network devices", resp.StatusCode(), resp.Body)
	}
}

func (c *Client) CreateVlan(ctx context.Context, body api.VlanWrite) (*api.Vlan, error) {
	resp, err := c.authenticated.CreateVlanWithResponse(ctx, body)
	if err != nil {
		return nil, err
	}

	switch resp.StatusCode() {
	case http.StatusCreated:
		return resp.JSON201, nil
	case http.StatusUnauthorized:
		return nil, ErrUnauthorized
	case http.StatusForbidden:
		return nil, ErrForbidden
	case http.StatusBadRequest:
		return nil, newAPIError(resp.StatusCode(), resp.JSON400)
	case http.StatusConflict:
		return nil, newAPIError(resp.StatusCode(), resp.JSON409)
	default:
		return nil, unexpectedStatus("create vlan", resp.StatusCode(), resp.Body)
	}
}

func (c *Client) UpdateVlan(ctx context.Context, id int64, body api.VlanPatch) (*api.Vlan, error) {
	resp, err := c.authenticated.PatchVlanWithResponse(ctx, api.VlanId(id), body)
	if err != nil {
		return nil, err
	}

	switch resp.StatusCode() {
	case http.StatusOK:
		return resp.JSON200, nil
	case http.StatusUnauthorized:
		return nil, ErrUnauthorized
	case http.StatusForbidden:
		return nil, ErrForbidden
	case http.StatusBadRequest:
		return nil, newAPIError(resp.StatusCode(), resp.JSON400)
	case http.StatusNotFound:
		return nil, newAPIError(resp.StatusCode(), &api.ErrorResponse{
			Code:    resp.JSON404.Code,
			Message: resp.JSON404.Message,
		})
	case http.StatusConflict:
		return nil, newAPIError(resp.StatusCode(), resp.JSON409)
	default:
		return nil, unexpectedStatus("update vlan", resp.StatusCode(), resp.Body)
	}
}

func (c *Client) DeleteVlan(ctx context.Context, id int64) error {
	resp, err := c.authenticated.DeleteVlanWithResponse(ctx, api.VlanId(id))
	if err != nil {
		return err
	}

	switch resp.StatusCode() {
	case http.StatusNoContent:
		return nil
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusForbidden:
		return ErrForbidden
	case http.StatusBadRequest:
		return newAPIError(resp.StatusCode(), resp.JSON400)
	case http.StatusConflict:
		return newAPIError(resp.StatusCode(), resp.JSON409)
	default:
		return unexpectedStatus("delete vlan", resp.StatusCode(), resp.Body)
	}
}

func (t *AuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	current, actualBackend, err := t.sessions.Load(t.profile, t.backend)
	if err != nil {
		return nil, err
	}

	requestWithAuth, err := cloneRequest(req)
	if err != nil {
		return nil, err
	}
	requestWithAuth.Header = req.Header.Clone()
	requestWithAuth.Header.Set("Authorization", authorizationHeader(current))

	resp, err := t.transport().RoundTrip(requestWithAuth)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusUnauthorized || isRefreshRequest(req.URL.Path) {
		return resp, nil
	}

	refreshed, refreshErr := t.refresh(req.Context(), current, actualBackend)
	if refreshErr != nil {
		return resp, nil
	}

	_ = resp.Body.Close()

	retryRequest, err := cloneRequest(req)
	if err != nil {
		return nil, err
	}
	retryRequest.Header = req.Header.Clone()
	retryRequest.Header.Set("Authorization", authorizationHeader(refreshed))

	return t.transport().RoundTrip(retryRequest)
}

func (t *AuthTransport) refresh(ctx context.Context, current session.Data, backend session.Backend) (session.Data, error) {
	t.refreshMu.Lock()
	defer t.refreshMu.Unlock()

	latest, latestBackend, err := t.sessions.Load(t.profile, backend)
	if err == nil && latest.RefreshToken != current.RefreshToken {
		return latest, nil
	}

	payload, err := json.Marshal(api.RefreshTokenRequest{RefreshToken: current.RefreshToken})
	if err != nil {
		return session.Data{}, fmt.Errorf("encode refresh request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		t.baseURL+"/api/v1/auth/tokens/refresh",
		bytes.NewReader(payload),
	)
	if err != nil {
		return session.Data{}, fmt.Errorf("build refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.transport().RoundTrip(req)
	if err != nil {
		return session.Data{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return session.Data{}, fmt.Errorf("read refresh response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var tokenResponse api.TokenResponse
		if err := json.Unmarshal(body, &tokenResponse); err != nil {
			return session.Data{}, fmt.Errorf("decode refresh response: %w", err)
		}

		data := TokenResponseToSessionData(&tokenResponse)
		if _, err := t.sessions.Save(t.profile, latestBackend, data); err != nil {
			return session.Data{}, err
		}
		return data, nil
	case http.StatusUnauthorized:
		_ = t.sessions.Delete(t.profile, latestBackend)
		return session.Data{}, ErrUnauthorized
	default:
		return session.Data{}, fmt.Errorf("refresh request failed with status %d", resp.StatusCode)
	}
}

func (t *AuthTransport) transport() http.RoundTripper {
	if t.base != nil {
		return t.base
	}
	return http.DefaultTransport
}

func cloneRequest(req *http.Request) (*http.Request, error) {
	clone := req.Clone(req.Context())
	if req.Body == nil {
		return clone, nil
	}
	if req.GetBody == nil {
		return nil, errors.New("request body cannot be replayed")
	}

	body, err := req.GetBody()
	if err != nil {
		return nil, fmt.Errorf("clone request body: %w", err)
	}
	clone.Body = body
	return clone, nil
}

func TokenResponseToSessionData(token *api.TokenResponse) session.Data {
	return session.Data{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		TokenType:    token.TokenType,
		ExpiresAt:    time.Now().Add(time.Duration(token.ExpiresInSeconds) * time.Second),
	}
}

func authorizationHeader(data session.Data) string {
	tokenType := strings.TrimSpace(data.TokenType)
	if tokenType == "" {
		tokenType = "Bearer"
	}
	return tokenType + " " + data.AccessToken
}

func isRefreshRequest(path string) bool {
	return strings.HasSuffix(path, "/api/v1/auth/tokens/refresh")
}

func newAPIError(statusCode int, errResp *api.ErrorResponse) error {
	if errResp == nil {
		return APIError{StatusCode: statusCode}
	}
	return APIError{
		StatusCode: statusCode,
		Code:       errResp.Code,
		Message:    errResp.Message,
	}
}

func unexpectedStatus(action string, statusCode int, body []byte) error {
	if len(body) == 0 {
		return fmt.Errorf("%s: unexpected status %d", action, statusCode)
	}
	return fmt.Errorf("%s: unexpected status %d: %s", action, statusCode, strings.TrimSpace(string(body)))
}
