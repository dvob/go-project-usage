package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/PuerkitoBio/goquery"
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
		token         string
		showRateLimit bool
	)

	flag.Usage = usage
	flag.StringVar(&token, "token", "", "Personal Access Token for Github. If not set environment variable GITHUB_TOKEN is used")
	flag.BoolVar(&showRateLimit, "limit", false, "Show Github rate limit stats and exit. Fetching the stats also costs one point.")
	flag.Parse()

	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}

	if token == "" {
		return fmt.Errorf("Github token not configured. Either set environment variable GITHUB_TOKEN or use flag -token")
	}

	if showRateLimit {
		stats, err := getRateLimitStats(context.Background(), token)
		if err != nil {
			return err
		}
		fmt.Printf("limit: %d\n", stats.limit)
		fmt.Printf("remaining: %d\n", stats.remaining)
		fmt.Printf("reset time: %s\n", stats.resetTime)
		return nil
	}

	if flag.NArg() != 1 {
		flag.Usage()
		fmt.Fprintln(os.Stderr, "")
		return fmt.Errorf("expect one package as argument")
	}

	pkgPath := flag.Arg(0)

	packages, err := getPackages(pkgPath)
	if err != nil {
		return err
	}

	if len(packages) >= 20000 {
		fmt.Fprintln(os.Stderr, "project is imported by more than 20000 packages. we only show results for the first 20000.")
	}

	githubRepos := getGithubRepos(packages)

	if len(githubRepos) == 0 {
		return fmt.Errorf("no projects found under https://pkg.go.dev/%s?tab=importedby", pkgPath)
	}

	projects, err := getAllProjects(context.Background(), githubRepos, token)
	if err != nil {
		return err
	}

	sort.Slice(projects, func(i, j int) bool {
		return projects[i].StargazerCount < projects[j].StargazerCount
	})

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
	fmt.Fprintln(w, "STARS\tFORKS\tPROJECT")
	for _, p := range projects {
		fmt.Fprintf(w, "%d\t%d\t%s\n", p.StargazerCount, p.ForkCount, p.URL)
	}
	w.Flush()
	return nil
}

func contains(strs []string, lookupStr string) bool {
	for _, str := range strs {
		if str == lookupStr {
			return true
		}
	}
	return false
}

func getGithubRepos(allPackages []string) []string {
	// TODO: lookup package location
	repos := []string{}
	for _, pkg := range allPackages {
		parts := strings.Split(pkg, "/")
		if parts[0] != "github.com" {
			continue
		}
		if len(parts) < 3 {
			continue
		}

		repo := strings.ToLower(parts[1] + "/" + parts[2])
		if contains(repos, repo) {
			continue
		}
		repos = append(repos, repo)
	}
	return repos
}

// getPackages returns all packages wich import this package
func getPackages(packagePath string) ([]string, error) {
	// Request the HTML page.
	url := fmt.Sprintf("https://pkg.go.dev/%s?tab=importedby", packagePath)

	res, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("status code error: %d %s", res.StatusCode, res.Status)
	}

	// Load the HTML document
	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return nil, err
	}

	projects := []string{}

	// Find the review items
	doc.Find(".Details-indent").Each(func(i int, s *goquery.Selection) {
		// For each item found, get the band and title
		projects = append(projects, s.Text())
	})

	return projects, nil
}
