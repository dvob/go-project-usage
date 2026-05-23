package cache

import "time"

type RepoInfo struct {
	Name           string    `json:"name"`
	URL            string    `json:"url"`
	ForkCount      int       `json:"fork_count"`
	IsFork         bool      `json:"is_fork"`
	IsArchived     bool      `json:"is_archived"`
	StargazerCount int       `json:"stargazer_count"`
	NotFound       bool      `json:"not_found,omitempty"`
	FetchedAt      time.Time `json:"fetched_at"`
}

// Cache provides access to cached repo metadata.
type Cache interface {
	// Get returns cached repos for the given names.
	// Repos not found or older than maxAge are omitted from the result.
	Get(repos []string, maxAge time.Duration) (map[string]RepoInfo, error)

	// Put stores repos in the cache.
	Put(repos []RepoInfo) error

	// List returns all repos in the cache.
	List() ([]RepoInfo, error)

	// Close releases any resources held by the cache.
	Close() error
}
