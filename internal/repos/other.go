package repos

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/sourcegraph/sourcegraph/internal/api"
	"github.com/sourcegraph/sourcegraph/internal/conf/reposource"
	"github.com/sourcegraph/sourcegraph/internal/extsvc"
	"github.com/sourcegraph/sourcegraph/internal/httpcli"
	"github.com/sourcegraph/sourcegraph/internal/jsonc"
	"github.com/sourcegraph/sourcegraph/internal/types"
	"github.com/sourcegraph/sourcegraph/lib/errors"
	"github.com/sourcegraph/sourcegraph/schema"
)

// A OtherSource yields repositories from a single Other connection configured
// in Sourcegraph via the external services configuration.
type OtherSource struct {
	svc    *types.ExternalService
	conn   *schema.OtherExternalServiceConnection
	client httpcli.Doer
}

// NewOtherSource returns a new OtherSource from the given external service.
func NewOtherSource(ctx context.Context, svc *types.ExternalService, cf *httpcli.Factory) (*OtherSource, error) {
	rawConfig, err := svc.Config.Decrypt(ctx)
	if err != nil {
		return nil, errors.Errorf("external service id=%d config error: %s", svc.ID, err)
	}
	var c schema.OtherExternalServiceConnection
	if err := jsonc.Unmarshal(rawConfig, &c); err != nil {
		return nil, errors.Wrapf(err, "external service id=%d config error", svc.ID)
	}

	if cf == nil {
		cf = httpcli.ExternalClientFactory
	}

	cli, err := cf.Doer()
	if err != nil {
		return nil, err
	}

	return &OtherSource{svc: svc, conn: &c, client: cli}, nil
}

// ListRepos returns all Other repositories accessible to all connections configured
// in Sourcegraph via the external services configuration.
func (s OtherSource) ListRepos(ctx context.Context, results chan SourceResult) {
	if len(s.conn.Repos) == 1 && (s.conn.Repos[0] == "src-expose" || s.conn.Repos[0] == "src-serve") {
		repos, err := s.srcExpose(ctx)
		if err != nil {
			results <- SourceResult{Source: s, Err: err}
		}
		for _, r := range repos {
			results <- SourceResult{Source: s, Repo: r}
		}
		return
	}

	urls, err := s.cloneURLs()
	if err != nil {
		results <- SourceResult{Source: s, Err: err}
		return
	}

	urn := s.svc.URN()
	for _, u := range urls {
		r, err := s.otherRepoFromCloneURL(urn, u)
		if err != nil {
			results <- SourceResult{Source: s, Err: err}
			return
		}
		results <- SourceResult{Source: s, Repo: r}
	}
}

// ExternalServices returns a singleton slice containing the external service.
func (s OtherSource) ExternalServices() types.ExternalServices {
	return types.ExternalServices{s.svc}
}

func (s OtherSource) cloneURLs() ([]*url.URL, error) {
	if len(s.conn.Repos) == 0 {
		return nil, nil
	}

	var base *url.URL
	if s.conn.Url != "" {
		var err error
		if base, err = url.Parse(s.conn.Url); err != nil {
			return nil, err
		}
	}

	cloneURLs := make([]*url.URL, 0, len(s.conn.Repos))
	for _, repo := range s.conn.Repos {
		cloneURL, err := otherRepoCloneURL(base, repo)
		if err != nil {
			return nil, err
		}
		cloneURLs = append(cloneURLs, cloneURL)
	}

	return cloneURLs, nil
}

func otherRepoCloneURL(base *url.URL, repo string) (*url.URL, error) {
	if base == nil {
		return url.Parse(repo)
	}
	return base.Parse(repo)
}

func (s OtherSource) otherRepoFromCloneURL(urn string, u *url.URL) (*types.Repo, error) {
	repoURL := u.String()
	repoSource := reposource.Other{OtherExternalServiceConnection: s.conn}
	repoName, err := repoSource.CloneURLToRepoName(u.String())
	if err != nil {
		return nil, err
	}
	repoURI, err := repoSource.CloneURLToRepoURI(u.String())
	if err != nil {
		return nil, err
	}
	u.Path, u.RawQuery = "", ""
	serviceID := u.String()

	return &types.Repo{
		Name: repoName,
		URI:  repoURI,
		ExternalRepo: api.ExternalRepoSpec{
			ID:          string(repoName),
			ServiceType: extsvc.TypeOther,
			ServiceID:   serviceID,
		},
		Sources: map[string]*types.SourceInfo{
			urn: {
				ID:       urn,
				CloneURL: repoURL,
			},
		},
		Metadata: &extsvc.OtherRepoMetadata{
			RelativePath: strings.TrimPrefix(repoURL, serviceID),
		},
	}, nil
}

func (s OtherSource) srcExpose(ctx context.Context) ([]*types.Repo, error) {
	req, err := http.NewRequest("GET", s.conn.Url+"/v1/list-repos", nil)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read response from src-expose")
	}

	var data struct {
		Items []*types.Repo
	}
	err = json.Unmarshal(b, &data)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to decode response from src-expose: %s", string(b))
	}

	clonePrefix := s.conn.Url
	if !strings.HasSuffix(clonePrefix, "/") {
		clonePrefix = clonePrefix + "/"
	}

	urn := s.svc.URN()
	for _, r := range data.Items {
		// The only required field is URI
		if r.URI == "" {
			return nil, errors.Errorf("repo without URI returned from src-expose: %+v", r)
		}

		// Fields that src-expose isn't allowed to control
		r.ExternalRepo = api.ExternalRepoSpec{
			ID:          r.URI,
			ServiceType: extsvc.TypeOther,
			ServiceID:   s.conn.Url,
		}

		cloneURL := clonePrefix + strings.TrimPrefix(r.URI, "/")
		// If the repo is not a bare repo, add a .git
		if !strings.HasSuffix(cloneURL, ".git") {
			cloneURL += "/.git"
		}
		r.Sources = map[string]*types.SourceInfo{
			urn: {
				ID:       urn,
				CloneURL: cloneURL,
			},
		}
		r.Metadata = &extsvc.OtherRepoMetadata{
			RelativePath: strings.TrimPrefix(cloneURL, s.conn.Url),
		}
		// The only required field left is Name
		name := string(r.Name)
		if name == "" {
			name = r.URI
		}
		// Remove any trailing .git in the name if exists (bare repos)
		r.Name = api.RepoName(strings.TrimSuffix(name, ".git"))

	}

	return data.Items, nil
}
