// Package pkgsite provides a client for the pkg.go.dev v1beta API.
// Based on golang.org/x/pkgsite/cmd/internal/pkgsite-cli/client.
package pkgsite

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const defaultServer = "https://pkg.go.dev"

// PackageLister returns packages that import a given package.
type PackageLister interface {
	GetImportedBy(ctx context.Context, pkgPath string) ([]string, error)
}

// Client fetches data from the pkg.go.dev v1beta API.
type Client struct {
	server     *url.URL
	httpClient *http.Client
}

// New creates a new Client.
func New() (*Client, error) {
	return NewWithServer(defaultServer)
}

// NewWithServer creates a new Client with a custom server URL.
func NewWithServer(server string) (*Client, error) {
	u, err := url.Parse(server)
	if err != nil {
		return nil, err
	}
	return &Client{
		server:     u,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// GetImportedBy returns the paths of packages that import the given package.
// It handles pagination automatically and returns all results.
func (c *Client) GetImportedBy(ctx context.Context, pkgPath string) ([]string, error) {
	var all []string
	var token string

	for {
		resp, err := c.getImportedByPage(ctx, pkgPath, token)
		if err != nil {
			return nil, err
		}
		all = append(all, resp.ImportedBy.Items...)
		token = resp.ImportedBy.NextPageToken
		if token == "" {
			break
		}
	}
	return all, nil
}

func (c *Client) getImportedByPage(ctx context.Context, path, pageToken string) (*packageImportedBy, error) {
	q := make(url.Values)
	q.Set("limit", "1000")
	if pageToken != "" {
		q.Set("token", pageToken)
	}
	u := c.server.JoinPath("v1beta", "imported-by", path)
	u.RawQuery = q.Encode()

	var resp packageImportedBy
	if err := c.get(ctx, u.String(), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) get(ctx context.Context, reqURL string, dst any) error {
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return err
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()
			wait := retryAfter(resp)
			slog.Info("pkgsite rate limited, retrying", "attempt", attempt+1, "wait", wait)
			select {
			case <-time.After(wait):
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
			var aerr apiError
			if json.Unmarshal(body, &aerr) == nil && aerr.Message != "" {
				return fmt.Errorf("pkgsite: %s (HTTP %d)", aerr.Message, aerr.Code)
			}
			return fmt.Errorf("pkgsite: HTTP %d: %s", resp.StatusCode, http.StatusText(resp.StatusCode))
		}
		return json.NewDecoder(resp.Body).Decode(dst)
	}
}

func retryAfter(resp *http.Response) time.Duration {
	if s := resp.Header.Get("Retry-After"); s != "" {
		if secs, err := strconv.Atoi(s); err == nil {
			return time.Duration(secs) * time.Second
		}
	}
	return 5 * time.Second
}

// Types based on golang.org/x/pkgsite/cmd/internal/pkgsite-cli/client.

type paginatedResponse struct {
	Items         []string `json:"items"`
	Total         int      `json:"total"`
	NextPageToken string   `json:"nextPageToken,omitempty"`
}

type packageImportedBy struct {
	ModulePath string             `json:"modulePath"`
	Version    string             `json:"version"`
	ImportedBy paginatedResponse  `json:"importedBy"`
}

type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

