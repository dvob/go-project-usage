package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var (
	githubURL = "https://api.github.com/graphql"
)

type project struct {
	Name             string `json:"nameWithOwner"`
	URL              string `json:"url"`
	ForkCount        int    `json:"forkCount"`
	IsFork           bool   `json:"isFork"`
	IsArchived       bool   `json:"isArchived"`
	IsInOrganization bool   `json:"isInOrganization"`
	StargazerCount   int    `json:"StargazerCount"`
}

type rateLimitStats struct {
	limit     int
	remaining int
	resetTime time.Time
}

func getRateLimitStats(ctx context.Context, token string) (*rateLimitStats, error) {
	req, err := http.NewRequestWithContext(ctx, "HEAD", githubURL, nil)
	req.Header.Add("Authorization", "Bearer "+token)

	client := &http.Client{}
	resp, err := client.Do(req)
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
	resetTime := time.Unix(rawResetTime, 0)
	return &rateLimitStats{
		limit:     limit,
		remaining: remaining,
		resetTime: resetTime,
	}, nil
}

// getProjects returns a list of projects for all projectIDs in the form OWNER/REPO
func getProjects(ctx context.Context, projectIDs []string, token string) ([]project, error) {
	query, err := buildQuery(projectIDs)
	if err != nil {
		return nil, err
	}

	type Request struct {
		Query string `json:"query"`
	}
	payload, err := json.Marshal(&Request{query})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", githubURL, bytes.NewBuffer(payload))
	req.Header.Add("Authorization", "Bearer "+token)

	client := &http.Client{}
	resp, err := client.Do(req)
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

	type Response struct {
		Data   map[string]project `json:"data"`
		Errors []Error            `json:"errors"`
	}

	response := &Response{}
	err = json.Unmarshal(payload, response)
	if err != nil {
		return nil, err
	}

	err = ignoreNotFoundErrs(response.Errors)
	if err != nil {
		return nil, err
	}

	projects := []project{}
	for _, project := range response.Data {
		// In the project array we usually have a couple of uninitialized projects for
		// the projects we could not find on Github. Most likely projects which do no
		// longer exist or are private.
		if project.Name == "" {
			continue
		}

		// different owner/name pairs can point to the same github project due to redirects.
		// e.g. peterbourgon/gokit -> go-kit/kit
		if containsProject(projects, project) {
			continue
		}
		projects = append(projects, project)
	}
	return projects, nil
}

func containsProject(ps []project, lookupProject project) bool {
	for _, p := range ps {
		if p.Name == lookupProject.Name {
			return true
		}
	}
	return false
}

type Error struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

func ignoreNotFoundErrs(errs []Error) error {
	realErrs := []Error{}
	for _, e := range errs {
		if e.Type == "NOT_FOUND" {
			continue
		}
		realErrs = append(realErrs, e)
	}

	if len(realErrs) != 0 {
		return fmt.Errorf("query failed. errors: %d. first error: %s", len(realErrs), realErrs[0].Message)
	}
	return nil
}

var queryTemplate = `{
{{ range . }}
{{ .Alias }}: repository(name: "{{ .Name }}", owner: "{{ .Owner }}") {nameWithOwner url forkCount isFork isArchived isInOrganization stargazerCount}
{{- end }}
}`

func buildQuery(projectIDs []string) (string, error) {
	type project struct {
		Alias string
		Name  string
		Owner string
	}
	projects := []project{}

	for i, projectID := range projectIDs {
		parts := strings.Split(projectID, "/")
		if len(parts) != 2 {
			return "", fmt.Errorf("projectID format error. got '%s' expects 'OWNER/REPONAME'", projectID)
		}

		projects = append(projects, project{
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
	err = t.Execute(buf, &projects)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}
