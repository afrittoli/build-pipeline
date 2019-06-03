package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-github/github"
	"go.uber.org/zap"
)

func TestGitHubParseURL(t *testing.T) {
	wantOwner := "owner"
	wantRepo := "repo"
	wantPR := 1

	for _, url := range []string{
		"https://github.com/owner/repo/pulls/1",
		"https://github.com/owner/repo/pulls/1/",
		"https://github.com/owner/repo/pulls/1/files",
		"http://github.com/owner/repo/pulls/1",
		"ssh://github.com/owner/repo/pulls/1",
		"https://example.com/owner/repo/pulls/1",
		"https://github.com/owner/repo/foo/1",
	} {
		t.Run(url, func(t *testing.T) {
			owner, repo, pr, err := parseGitHubURL(url)
			if err != nil {
				t.Fatal(err)
			}
			if owner != wantOwner {
				t.Errorf("Owner: %s, want: %s", owner, wantOwner)
			}
			if repo != wantRepo {
				t.Errorf("Repo: %s, want: %s", repo, wantRepo)
			}
			if pr != wantPR {
				t.Errorf("PR Number: %d, want: %d", pr, wantPR)
			}
		})
	}
}

func TestGitHubParseURL_errors(t *testing.T) {
	for _, url := range []string{
		"",
		"https://github.com/owner/repo",
		"https://github.com/owner/repo/pulls/foo",
	} {
		t.Run(url, func(t *testing.T) {
			if o, r, pr, err := parseGitHubURL(url); err == nil {
				t.Errorf("Expected error, got (%s, %s, %d)", o, r, pr)
			}
		})
	}
}

func newClient(gh *FakeGitHub) (*github.Client, func()) {
	s := httptest.NewServer(gh)
	client := github.NewClient(s.Client())
	u, err := url.Parse(fmt.Sprintf("%s/", s.URL))
	if err != nil {
		panic(err)
	}
	client.BaseURL = u
	return client, s.Close
}

func TestGitHub(t *testing.T) {
	ctx := context.Background()
	gh := NewFakeGitHub()
	client, close := newClient(gh)
	defer close()

	owner := "foo"
	repo := "bar"
	prNum := 1

	pr := &github.PullRequest{
		ID:      github.Int64(int64(prNum)),
		HTMLURL: github.String(fmt.Sprintf("https://github.com/%s/%s/pull/%d", owner, repo, prNum)),
		Base: &github.PullRequestBranch{
			User: &github.User{
				Login: github.String(owner),
			},
			Repo: &github.Repository{
				Name:     github.String(repo),
				CloneURL: github.String(fmt.Sprintf("https://github.com/%s/%s", owner, repo)),
			},
			Ref: github.String("master"),
			SHA: github.String("1"),
		},
		Head: &github.PullRequestBranch{
			User: &github.User{
				Login: github.String("baz"),
			},
			Repo: &github.Repository{
				Name:     github.String(repo),
				CloneURL: github.String(fmt.Sprintf("https://github.com/baz/%s", repo)),
			},
			Ref: github.String("feature"),
			SHA: github.String("2"),
		},
	}
	gh.AddPullRequest(pr)
	comment := &github.IssueComment{
		ID:   github.Int64(1),
		Body: github.String("hello world!"),
	}
	gh.AddComment(owner, repo, int64(prNum), comment)

	h, err := NewGitHubHandler(ctx, zap.NewNop().Sugar(), pr.GetHTMLURL())
	if err != nil {
		t.Fatal(err)
	}
	// Override GitHub client to point to local test server.
	h.Client = client

	dir := os.TempDir()
	if err := h.Download(ctx, dir); err != nil {
		t.Fatal(err)
	}

	prPath := filepath.Join(dir, "pr.json")
	rawPRPath := filepath.Join(dir, "github/pr.json")
	rawCommentPath := filepath.Join(dir, "github/comments/1.json")

	wantPR := &PullRequest{
		Type: "github",
		ID:   int64(prNum),
		Head: &GitReference{
			Repo:   pr.GetHead().GetRepo().GetCloneURL(),
			Branch: pr.GetHead().GetRef(),
			SHA:    pr.GetHead().GetSHA(),
		},
		Base: &GitReference{
			Repo:   pr.GetBase().GetRepo().GetCloneURL(),
			Branch: pr.GetBase().GetRef(),
			SHA:    pr.GetBase().GetSHA(),
		},
		Comments: []*Comment{{
			ID:     comment.GetID(),
			Author: comment.GetUser().GetLogin(),
			Text:   comment.GetBody(),
			Raw:    rawCommentPath,
		}},
		Labels: []*Label{},
		Raw:    rawPRPath,
	}

	gotPR := new(PullRequest)
	diffFile(t, prPath, wantPR, gotPR)
	diffFile(t, rawPRPath, pr, new(github.PullRequest))
	if rawPRPath != gotPR.Raw {
		t.Errorf("Raw PR path: want [%s], got [%s]", rawPRPath, gotPR.Raw)
	}
	diffFile(t, rawCommentPath, comment, new(github.IssueComment))
	if rawCommentPath != gotPR.Comments[0].Raw {
		t.Errorf("Raw PR path: want [%s], got [%s]", rawCommentPath, gotPR.Comments[0].Raw)
	}

	// Upload

	// Update labels
	gotPR.Labels = []*Label{{
		Text: "tacocat",
	}}
	gotPR.Comments = append(gotPR.Comments, &Comment{
		Text: "abc123",
	})
	f, err := os.Create(prPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(f).Encode(gotPR); err != nil {
		t.Fatal(err)
	}
	if err := h.Upload(ctx, dir); err != nil {
		t.Fatal(err)
	}
	ghPR, _, err := client.PullRequests.Get(ctx, owner, repo, prNum)
	if err != nil {
		t.Fatal(err)
	}
	pr.Labels = []*github.Label{{
		Name: github.String(gotPR.Labels[0].Text),
	}}
	if diff := cmp.Diff(pr, ghPR); diff != "" {
		t.Errorf("Upload PR diff: %s", diff)
	}

	ghComments, _, err := client.Issues.ListComments(ctx, owner, repo, prNum, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantComments := []*github.IssueComment{comment, &github.IssueComment{
		ID:   github.Int64(2),
		Body: github.String(gotPR.Comments[1].Text),
	}}
	if diff := cmp.Diff(wantComments, ghComments); diff != "" {
		t.Errorf("Upload comment diff: %s", diff)
	}

}

func diffFile(t *testing.T, path string, want interface{}, got interface{}) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewDecoder(f).Decode(got); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("PR Diff: %s", diff)
	}
}
