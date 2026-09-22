package infisical

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	userAgent      = "lazylock (+https://github.com/joaoseixas88/lazylock)"
	maxErrorBody   = 8 << 10
	maxSuccessBody = 32 << 20
)

// Client talks to one Infisical instance. The zero value is not usable; build
// it with NewClient.
type Client struct {
	base string
	http *http.Client

	mu    sync.RWMutex
	token string
}

type Option func(*Client)

// WithTransport swaps only the round tripper, which is how the tests point the
// client at an httptest.Server. It deliberately does not take a whole
// *http.Client: the timeouts and the no-redirect policy below are part of what
// this package guarantees, and a test that replaced them would stop exercising
// them.
func WithTransport(rt http.RoundTripper) Option {
	return func(c *Client) { c.http.Transport = rt }
}

func NewClient(baseURL string, opts ...Option) *Client {
	c := &Client{
		base: strings.TrimSuffix(baseURL, "/"),
		http: &http.Client{
			Timeout: 20 * time.Second,
			// Never follow a redirect: doing so would replay the Authorization
			// header to whatever host the redirect names, which on a
			// self-hosted box behind a proxy is not necessarily the one the
			// user configured.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
				TLSHandshakeTimeout:   5 * time.Second,
				ResponseHeaderTimeout: 15 * time.Second,
				ForceAttemptHTTP2:     true,
				MaxIdleConnsPerHost:   4,
			},
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Client) SetToken(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = token
}

func (c *Client) Token() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.token
}

// get issues an authenticated GET and decodes a JSON body into out.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	endpoint := c.base + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build request for %s: %w", path, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if token := c.Token(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		// url.Error stringifies the full URL, query values and all. Replace it
		// with the path so nothing reaches the UI that should not.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			return fmt.Errorf("GET %s: %w", path, urlErr.Err)
		}
		return fmt.Errorf("GET %s: %w", path, err)
	}
	defer func() {
		io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBody))
		resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.decodeError(resp, http.MethodGet, path)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxSuccessBody)).Decode(out); err != nil {
		return fmt.Errorf("decode %s response: %w", path, err)
	}
	return nil
}

func (c *Client) decodeError(resp *http.Response, method, path string) error {
	apiErr := &APIError{StatusCode: resp.StatusCode, Method: method, Path: path}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))

	var wire wireError
	if err := json.Unmarshal(body, &wire); err == nil {
		apiErr.Err = wire.Error
		apiErr.Message = flattenMessage(wire.Message)
		apiErr.ReqID = wire.ReqID
		return apiErr
	}
	// A self-hosted instance behind a proxy answers with HTML on a bad gateway.
	// Never put that in a pane.
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "json") {
		return apiErr
	}
	apiErr.Message = truncate(string(body), 200)
	return apiErr
}
