package main

import "testing"

func buildQuery(t *testing.T) {
	pkgs, err := getPackages("github.com/nats-io/nats.go")
	if err != nil {
		t.Fatal(err)
	}

	repos := getGithubRepos(pkgs)

	query, err := buildQuery(repos)
	if err != nil {
		t.Fatal(err)
	}
}
