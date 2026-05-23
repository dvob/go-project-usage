package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/dvob/go-project-usage/internal/cache"
	"github.com/dvob/go-project-usage/internal/github"
	"github.com/dvob/go-project-usage/internal/pkgsite"
)

func main() {
	err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: go-project-usage <module>\n")
	fmt.Fprintf(os.Stderr, "options:\n")
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, "examples:\n")
	fmt.Fprintf(os.Stderr, "\tgo-project-usage github.com/nats-io/nats.go\n")
}

func run() error {
	var (
		showRateLimit bool
		listCache     bool
		clientID      = "Iv23lizaK4VTpzTuiX9h"
	)

	flag.Usage = usage
	flag.BoolVar(&showRateLimit, "limit", false, "Show Github rate limit stats and exit. Fetching the stats also costs one point.")
	flag.BoolVar(&listCache, "list", false, "List all entries in the cache and exit.")
	flag.Parse()

	if listCache {
		return listCacheEntries()
	}

	// Token source.
	var tokenFunc github.TokenFunc
	if envToken := os.Getenv("GITHUB_TOKEN"); envToken != "" {
		tokenFunc = func(ctx context.Context) (string, error) {
			return envToken, nil
		}
	} else {
		if clientID == "" {
			clientID = os.Getenv("GITHUB_CLIENT_ID")
		}
		if clientID == "" {
			return fmt.Errorf("set GITHUB_TOKEN or provide GITHUB_CLIENT_ID for device flow login")
		}

		tokenCacheFile, err := github.DefaultCacheFile()
		if err != nil {
			return err
		}
		ts := &github.TokenSource{
			ClientID:  clientID,
			CacheFile: tokenCacheFile,
		}
		tokenFunc = func(ctx context.Context) (string, error) {
			t, err := ts.Token(ctx)
			if err != nil {
				return "", err
			}
			return t.AccessToken, nil
		}
	}

	// Cache.
	cacheDir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	c, err := cache.NewBoltCache(cacheDir + "/go-project-usage/cache.db")
	if err != nil {
		return err
	}
	defer c.Close()

	// GitHub client.
	ghClient := &github.Client{
		Token:  tokenFunc,
		Cache:  c,
		MaxAge: 6 * 30 * 24 * time.Hour,
	}

	if showRateLimit {
		stats, err := ghClient.GetRateLimitStats(context.Background())
		if err != nil {
			return err
		}
		fmt.Printf("limit: %d\n", stats.Limit)
		fmt.Printf("remaining: %d\n", stats.Remaining)
		fmt.Printf("reset time: %s\n", stats.ResetTime)
		return nil
	}

	if flag.NArg() != 1 {
		flag.Usage()
		fmt.Fprintln(os.Stderr, "")
		return fmt.Errorf("expect one package as argument")
	}

	pkgPath := flag.Arg(0)

	// Package source.
	pkgClient, err := pkgsite.New()
	if err != nil {
		return err
	}

	slog.Info("get packages from pkgsite")
	packages, err := pkgClient.GetImportedBy(context.Background(), pkgPath)
	if err != nil {
		return err
	}

	githubRepos := github.ExtractRepos(packages)

	if len(githubRepos) == 0 {
		return fmt.Errorf("no projects found for %s", pkgPath)
	}
	log.Printf("%d Github repos are using this package", len(githubRepos))

	repos, err := ghClient.GetRepoInfos(context.Background(), githubRepos)
	if err != nil {
		return err
	}

	sort.Slice(repos, func(i, j int) bool {
		return repos[i].StargazerCount < repos[j].StargazerCount
	})

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
	fmt.Fprintln(w, "STARS\tFORKS\tPROJECT")
	for _, r := range repos {
		fmt.Fprintf(w, "%d\t%d\t%s\n", r.StargazerCount, r.ForkCount, r.URL)
	}
	w.Flush()
	return nil
}

func listCacheEntries() error {
	cacheDir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	c, err := cache.NewBoltCache(cacheDir + "/go-project-usage/cache.db")
	if err != nil {
		return err
	}
	defer c.Close()

	repos, err := c.List()
	if err != nil {
		return err
	}

	sort.Slice(repos, func(i, j int) bool {
		return repos[i].StargazerCount < repos[j].StargazerCount
	})

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
	fmt.Fprintln(w, "STARS\tFORKS\tFETCHED\tPROJECT")
	for _, r := range repos {
		fmt.Fprintf(w, "%d\t%d\t%s\t%s\n", r.StargazerCount, r.ForkCount, r.FetchedAt.Format(time.RFC3339), r.Name)
	}
	w.Flush()
	return nil
}
