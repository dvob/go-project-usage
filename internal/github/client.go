package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dvob/go-project-usage/internal/cache"
	"golang.org/x/sync/errgroup"
)

var graphqlURL = "https://api.github.com/graphql"

// TokenFunc returns a GitHub API token.
type TokenFunc func(ctx context.Context) (string, error)

// Client fetches repository metadata from the GitHub GraphQL API,
// with caching to avoid redundant requests.
type Client struct {
	Token  TokenFunc
	Cache  cache.Cache
	MaxAge time.Duration
}

// GetRepoInfos returns metadata for the given repos (in "owner/repo" format).
// Cached entries are returned directly; uncached repos are fetched from GitHub
// in chunks and pushed to the cache immediately.
func (c *Client) GetRepoInfos(ctx context.Context, repos []string) ([]cache.RepoInfo, error) {
	token, err := c.Token(ctx)
	if err != nil {
		return nil, err
	}

	cached, err := c.Cache.Get(repos, c.MaxAge)
	if err != nil {
		return nil, err
	}

	var uncached []string
	for _, repo := range repos {
		if _, ok := cached[repo]; !ok {
			uncached = append(uncached, repo)
		}
	}

	slog.Info("get github metadata", "total", len(repos), "cached", len(cached), "fetching", len(uncached))

	// somewhere between 2125 and 2250 is the limit from what Github can handle.
	// If we try to query more than that Github returns a HTTP 500 error.
	// 2026-05-23: Git hub got much worse so we only can do 100 at a time
	chunkSize := 100

	errGroup, ctx := errgroup.WithContext(ctx)
	errGroup.SetLimit(3)
	mu := &sync.Mutex{}

	seen := make(map[string]bool)
	result := make([]cache.RepoInfo, 0, len(repos))

	for _, ri := range cached {
		if ri.NotFound {
			continue
		}
		seen[strings.ToLower(ri.Name)] = true
		result = append(result, ri)
	}

	for i := 0; i < len(uncached); i += chunkSize {
		batch := uncached[i:min(i+chunkSize, len(uncached))]
		errGroup.Go(func() error {
			fetched, err := fetchRepos(ctx, batch, token)
			if err != nil {
				return err
			}

			now := time.Now()
			foundNames := make(map[string]bool, len(fetched))
			toCache := make([]cache.RepoInfo, 0, len(batch))

			for _, r := range fetched {
				name := strings.ToLower(r.NameWithOwner)
				foundNames[name] = true
				toCache = append(toCache, cache.RepoInfo{
					Name:           name,
					URL:            r.URL,
					ForkCount:      r.ForkCount,
					IsFork:         r.IsFork,
					IsArchived:     r.IsArchived,
					StargazerCount: r.StargazerCount,
					FetchedAt:      now,
				})
			}

			// Cache negative lookups for repos not returned by GitHub.
			for _, repo := range batch {
				// A repo may be returned under a different name due to redirects,
				// so check both the requested name and the found names.
				if !foundNames[repo] {
					toCache = append(toCache, cache.RepoInfo{
						Name:      repo,
						NotFound:  true,
						FetchedAt: now,
					})
				}
			}

			if err := c.Cache.Put(toCache); err != nil {
				return err
			}

			mu.Lock()
			for _, ri := range toCache {
				if ri.NotFound {
					continue
				}
				key := strings.ToLower(ri.Name)
				// different owner/name pairs can point to the same github project due to redirects.
				// e.g. peterbourgon/gokit -> go-kit/kit
				if !seen[key] {
					seen[key] = true
					result = append(result, ri)
				}
			}
			mu.Unlock()
			return nil
		})
	}
	if err := errGroup.Wait(); err != nil {
		return nil, err
	}
	return result, nil
}

type RateLimitStats struct {
	Limit     int
	Remaining int
	ResetTime time.Time
}

func (c *Client) GetRateLimitStats(ctx context.Context) (*RateLimitStats, error) {
	token, err := c.Token(ctx)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "HEAD", graphqlURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	limit, err := strconv.Atoi(resp.Header.Get("X-RateLimit-Limit"))
	if err != nil {
		return nil, err
	}
	remaining, err := strconv.Atoi(resp.Header.Get("X-RateLimit-Remaining"))
	if err != nil {
		return nil, err
	}
	rawResetTime, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64)
	if err != nil {
		return nil, err
	}
	return &RateLimitStats{
		Limit:     limit,
		Remaining: remaining,
		ResetTime: time.Unix(rawResetTime, 0),
	}, nil
}

// ExtractRepos extracts unique "owner/repo" strings from a list of Go package paths,
// keeping only packages hosted on github.com.
func ExtractRepos(packages []string) []string {
	seen := make(map[string]bool)
	var repos []string
	for _, pkg := range packages {
		parts := strings.Split(pkg, "/")
		if parts[0] != "github.com" || len(parts) < 3 {
			continue
		}
		repo := strings.ToLower(parts[1] + "/" + parts[2])
		if !seen[repo] {
			seen[repo] = true
			repos = append(repos, repo)
		}
	}
	return repos
}

// graphqlRepo is the JSON shape returned by the GitHub GraphQL API.
type graphqlRepo struct {
	NameWithOwner  string `json:"nameWithOwner"`
	URL            string `json:"url"`
	ForkCount      int    `json:"forkCount"`
	IsFork         bool   `json:"isFork"`
	IsArchived     bool   `json:"isArchived"`
	StargazerCount int    `json:"stargazerCount"`
}

type graphqlError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

func fetchRepos(ctx context.Context, repos []string, token string) ([]graphqlRepo, error) {
	query, err := buildQuery(repos)
	if err != nil {
		return nil, err
	}

	type request struct {
		Query string `json:"query"`
	}
	payload, err := json.Marshal(&request{query})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", graphqlURL, bytes.NewBuffer(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Add("Authorization", "Bearer "+token)

	slog.Info("perform request", "repos", len(repos))
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	payload, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read body: %w", err)
	}

	if resp.StatusCode > 399 {
		return nil, fmt.Errorf("http request failed with status: %d. body: %s", resp.StatusCode, string(payload))
	}
	slog.Info("request done", "duration", time.Since(start))

	type response struct {
		Data   map[string]graphqlRepo `json:"data"`
		Errors []graphqlError         `json:"errors"`
	}

	var res response
	if err := json.Unmarshal(payload, &res); err != nil {
		return nil, err
	}

	if err := checkErrors(res.Errors); err != nil {
		return nil, err
	}

	result := make([]graphqlRepo, 0, len(res.Data))
	for _, repo := range res.Data {
		if repo.NameWithOwner == "" {
			continue
		}
		result = append(result, repo)
	}
	return result, nil
}

func checkErrors(errs []graphqlError) error {
	var real []graphqlError
	for _, e := range errs {
		if e.Type != "NOT_FOUND" {
			real = append(real, e)
		}
	}
	if len(real) > 0 {
		return fmt.Errorf("query failed. errors: %d. first error: %s", len(real), real[0].Message)
	}
	return nil
}

var queryTemplate = `{
{{ range . }}
{{ .Alias }}: repository(name: "{{ .Name }}", owner: "{{ .Owner }}") {nameWithOwner url forkCount isFork isArchived stargazerCount}
{{- end }}
}`

func buildQuery(repos []string) (string, error) {
	type entry struct {
		Alias string
		Name  string
		Owner string
	}
	entries := make([]entry, 0, len(repos))

	for i, repo := range repos {
		parts := strings.Split(repo, "/")
		if len(parts) != 2 {
			return "", fmt.Errorf("repo format error. got '%s' expects 'OWNER/REPONAME'", repo)
		}
		entries = append(entries, entry{
			Alias: "_" + strconv.Itoa(i),
			Name:  parts[1],
			Owner: parts[0],
		})
	}

	t, err := template.New("query").Parse(queryTemplate)
	if err != nil {
		return "", err
	}

	buf := &bytes.Buffer{}
	if err := t.Execute(buf, &entries); err != nil {
		return "", err
	}
	return buf.String(), nil
}
