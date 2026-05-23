package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	deviceCodeURL = "https://github.com/login/device/code"
	tokenURL      = "https://github.com/login/oauth/access_token"
)

type Token struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
}

type deviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
	Error       string `json:"error"`
}

// TokenSource provides GitHub access tokens with caching.
type TokenSource struct {
	ClientID  string
	CacheFile string
}

// DefaultCacheFile returns the default token cache path for GitHub.
func DefaultCacheFile() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "go-project-usage", "github-token.json"), nil
}

// Token returns a valid access token. It uses a cached token if available,
// otherwise performs a device flow login.
func (ts *TokenSource) Token(ctx context.Context) (*Token, error) {
	cached, _ := loadToken(ts.CacheFile)
	if cached != nil && cached.AccessToken != "" {
		return cached, nil
	}

	token, err := Login(ctx, ts.ClientID)
	if err != nil {
		return nil, err
	}
	saveToken(ts.CacheFile, token)
	return token, nil
}

// Login performs the GitHub device flow:
// it requests a device code, shows the user a URL and code to enter,
// then polls for the token.
func Login(ctx context.Context, clientID string) (*Token, error) {
	dc, err := requestDeviceCode(ctx, clientID)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Go to: %s\n", dc.VerificationURI)
	fmt.Printf("Enter code: %s\n", dc.UserCode)

	return pollForToken(ctx, clientID, dc)
}

func requestDeviceCode(ctx context.Context, clientID string) (*deviceCodeResponse, error) {
	data := url.Values{
		"client_id": {clientID},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", deviceCodeURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read device code response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device code request failed (status %d): %s", resp.StatusCode, string(body))
	}

	var dc deviceCodeResponse
	if err := json.Unmarshal(body, &dc); err != nil {
		return nil, fmt.Errorf("failed to parse device code response: %w", err)
	}
	return &dc, nil
}

func pollForToken(ctx context.Context, clientID string, dc *deviceCodeResponse) (*Token, error) {
	interval := time.Duration(dc.Interval) * time.Second
	if interval == 0 {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(time.Duration(dc.ExpiresIn) * time.Second)

	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("device code expired, please try again")
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}

		data := url.Values{
			"client_id":   {clientID},
			"device_code": {dc.DeviceCode},
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		}

		req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}

		var tr tokenResponse
		if err := json.Unmarshal(body, &tr); err != nil {
			return nil, fmt.Errorf("failed to parse token response: %w", err)
		}

		switch tr.Error {
		case "":
			if tr.AccessToken == "" {
				return nil, fmt.Errorf("empty access token in response: %s", string(body))
			}
			return &Token{
				AccessToken: tr.AccessToken,
				TokenType:   tr.TokenType,
				Scope:       tr.Scope,
			}, nil
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5 * time.Second
			continue
		case "expired_token":
			return nil, fmt.Errorf("device code expired, please try again")
		case "access_denied":
			return nil, fmt.Errorf("login denied by user")
		default:
			return nil, fmt.Errorf("token error: %s", tr.Error)
		}
	}
}

func loadToken(path string) (*Token, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var token Token
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, err
	}
	return &token, nil
}

func saveToken(path string, token *Token) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
