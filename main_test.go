package main

import (
	"reflect"
	"testing"
)

func Test_getGithubRepos(t *testing.T) {
	packages := []string{
		"github.com/dvob/bla",
		"github.com/dvob/Bla",
		"github.com/dvob/bla/foo/bar",
		"not-github.com/foo/bli/bla/blo",
		"github.com/dvob/mod1",
	}

	expectedOutput := []string{
		"dvob/bla",
		"dvob/mod1",
	}

	githubRepos := getGithubRepos(packages)

	if !reflect.DeepEqual(expectedOutput, githubRepos) {
		t.Fatal("got", githubRepos, "expected", expectedOutput)
	}
}
