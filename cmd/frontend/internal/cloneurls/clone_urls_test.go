package cloneurls

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcegraph/sourcegraph/internal/api"
	"github.com/sourcegraph/sourcegraph/internal/database"
	"github.com/sourcegraph/sourcegraph/internal/extsvc"
	"github.com/sourcegraph/sourcegraph/internal/types"
)

func TestReposourceCloneURLToRepoName(t *testing.T) {
	ctx := context.Background()

	externalServices := database.NewMockExternalServiceStore()
	externalServices.ListFunc.SetDefaultReturn(
		[]*types.ExternalService{{
			ID:          1,
			Kind:        extsvc.KindGitHub,
			DisplayName: "GITHUB #1",
			Config:      extsvc.NewUnencryptedConfig(`{"url": "https://github.example.com", "repositoryQuery": ["none"], "token": "abc"}`),
		}},
		nil,
	)

	db := database.NewMockDB()
	db.ExternalServicesFunc.SetDefaultReturn(externalServices)
	db.ReposFunc.SetDefaultReturn(database.NewMockRepoStore())

	tests := []struct {
		name         string
		cloneURL     string
		wantRepoName api.RepoName
	}{
		{
			name:     "no match",
			cloneURL: "https://gitlab.com/user/repo",
		},
		{
			name:         "match existing external service",
			cloneURL:     "https://github.example.com/user/repo.git",
			wantRepoName: api.RepoName("github.example.com/user/repo"),
		},
		{
			name:         "fallback for github.com",
			cloneURL:     "https://github.com/user/repo",
			wantRepoName: api.RepoName("github.com/user/repo"),
		},
		{
			name:         "relatively-pathed submodule",
			cloneURL:     "../../a/b/c.git",
			wantRepoName: api.RepoName("github.example.com/a/b/c"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repoName, err := ReposourceCloneURLToRepoName(ctx, db, test.cloneURL)
			if err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff(test.wantRepoName, repoName); diff != "" {
				t.Fatalf("RepoName mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
