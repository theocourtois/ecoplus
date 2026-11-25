package provider

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

	"github.com/theocourtois/ecoplus/internal/api"
)

const (
	maxAttempts   = 8
	maxRetryDelay = 30 * time.Second
)

type Client struct {
	endpoint string
	token    string
	version  string
	http     *http.Client
	backoff  time.Duration
}

func NewClient(endpoint, token, version string, backoff time.Duration) *Client {
	return &Client{
		endpoint: strings.TrimRight(endpoint, "/"), token: token, version: version, backoff: backoff,
		http: &http.Client{Timeout: 5 * time.Minute},
	}
}

func (c *Client) Create(ctx context.Context, req api.ServerRequest) (api.Server, error) {
	var out api.Server
	err := c.do(ctx, http.MethodPost, api.PathServers, "", req, &out)
	return out, err
}

func (c *Client) Get(ctx context.Context, id int64, lease string) (api.Server, error) {
	var out api.Server
	err := c.do(ctx, http.MethodGet, serverPath(id), lease, nil, &out)
	return out, err
}

func (c *Client) Patch(ctx context.Context, id int64, lease string, patch api.ServerPatch) (api.Server, error) {
	var out api.Server
	err := c.do(ctx, http.MethodPatch, serverPath(id), lease, patch, &out)
	return out, err
}

func (c *Client) Delete(ctx context.Context, id int64, lease string) error {
	err := c.do(ctx, http.MethodDelete, serverPath(id), lease, nil, nil)
	if IsNotFound(err) {
		return nil
	}
	return err
}

func IsNotFound(err error) bool {
	var apiErr *api.Error
	return errors.As(err, &apiErr) && apiErr.Code == api.CodeNotFound
}

func serverPath(id int64) string {
	return api.PathServers + "/" + strconv.FormatInt(id, 10)
}

func (c *Client) do(ctx context.Context, method, path, lease string, body, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
	}
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		delay, err := c.once(ctx, method, path, lease, payload, out)
		if err == nil {
			return nil
		}
		var apiErr *api.Error
		if !errors.As(err, &apiErr) || !apiErr.Retryable() {
			return err
		}
		lastErr = err
		if werr := c.wait(ctx, attempt, delay); werr != nil {
			return fmt.Errorf("%w (last error: %w)", werr, lastErr)
		}
	}
	return lastErr
}

func (c *Client) wait(ctx context.Context, attempt int, hint time.Duration) error {
	delay := c.backoff << attempt
	if hint > delay && c.backoff >= time.Second {
		delay = hint
	}
	delay = min(delay, maxRetryDelay)
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) once(ctx context.Context, method, path, lease string, payload []byte, out any) (time.Duration, error) {
	target, err := url.JoinPath(c.endpoint, path)
	if err != nil {
		return 0, fmt.Errorf("build url: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set(api.HeaderAuthorization, "Bearer "+c.token)
	req.Header.Set(api.HeaderUserAgent, api.UserAgentPrefix+c.version)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if lease != "" {
		req.Header.Set(api.HeaderLease, lease)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, api.NewError(api.CodeUpstream, "controller unreachable: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return retryHint(resp), decodeResponse(resp, out)
}

func retryHint(resp *http.Response) time.Duration {
	if secs, err := strconv.Atoi(resp.Header.Get(api.HeaderRetryAfter)); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
}

func decodeResponse(resp *http.Response, out any) error {
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return api.NewError(api.CodeUpstream, "read response: %v", err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out == nil || len(raw) == 0 {
			return nil
		}
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
		return nil
	}
	var apiErr api.Error
	if err := json.Unmarshal(raw, &apiErr); err != nil || apiErr.Code == "" {
		code := api.CodeInternal
		if resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusGatewayTimeout {
			code = api.CodeUpstream
		}
		return api.NewError(code, "controller returned status %d", resp.StatusCode)
	}
	return &apiErr
}
