package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/oauth2"

	"github.com/google/go-github/github"
	"go.uber.org/zap"
)

// GitHubHandler handles interactions with the GitHub API.
type GitHubHandler struct {
	*github.Client

	owner, repo string
	prNum       int

	Logger *zap.SugaredLogger
}

// NewGitHubHandler initializes a new handler for interacting with GitHub
// resources.
func NewGitHubHandler(ctx context.Context, logger *zap.SugaredLogger, rawURL string) (*GitHubHandler, error) {
	token := strings.TrimSpace(os.Getenv("GITHUBOAUTHTOKEN"))
	var hc *http.Client
	if token != "" {
		ts := oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: token},
		)
		hc = oauth2.NewClient(ctx, ts)
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	split := strings.Split(u.Path, "/")
	if len(split) != 5 {
		return nil, fmt.Errorf("could not determine PR from URL: %v", rawURL)
	}
	owner, repo, pr := split[1], split[2], split[4]
	prNumber, err := strconv.Atoi(pr)
	if err != nil {
		return nil, fmt.Errorf("error parsing PR number: %s", pr)
	}

	return &GitHubHandler{
		Client: github.NewClient(hc),
		Logger: logger,
		owner:  owner,
		repo:   repo,
		prNum:  prNumber,
	}, nil
}

// writeJSON writes an arbitrary interface to the given path.
func writeJSON(path string, i interface{}) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	return json.NewEncoder(f).Encode(i)
}

// Download fetches and stores the desired pull request.
func (h *GitHubHandler) Download(ctx context.Context, path string) error {
	rawPrefix := filepath.Join(path, "github")
	if err := os.MkdirAll(rawPrefix, 0755); err != nil {
		return err
	}

	// Pull request
	gpr, _, err := h.PullRequests.Get(ctx, h.owner, h.repo, h.prNum)
	if err != nil {
		return err
	}
	rawPR := filepath.Join(rawPrefix, "pr.json")
	if err := writeJSON(rawPR, gpr); err != nil {
		return err
	}
	pr := &PullRequest{
		Type: "github",
		ID:   gpr.GetID(),
		Head: &GitReference{
			Repo:   gpr.GetHead().GetRepo().GetCloneURL(),
			Branch: gpr.GetHead().GetRef(),
			SHA:    gpr.GetHead().GetSHA(),
		},
		Base: &GitReference{
			Repo:   gpr.GetBase().GetRepo().GetCloneURL(),
			Branch: gpr.GetBase().GetRef(),
			SHA:    gpr.GetBase().GetSHA(),
		},

		Raw: rawPR,
	}

	// Labels
	pr.Labels = make([]*Label, 0, len(gpr.Labels))
	for _, l := range gpr.Labels {
		pr.Labels = append(pr.Labels, &Label{
			Text: l.GetName(),
		})
	}

	// Comments
	commentsPrefix := filepath.Join(rawPrefix, "comments")
	for _, p := range []string{commentsPrefix} {
		if err := os.MkdirAll(p, 0755); err != nil {
			return err
		}
	}
	ic, _, err := h.Issues.ListComments(ctx, h.owner, h.repo, h.prNum, nil)
	if err != nil {
		return err
	}
	pr.Comments = make([]*Comment, 0, len(ic))
	for _, c := range ic {
		rawComment := filepath.Join(commentsPrefix, fmt.Sprintf("%d.json", c.GetID()))
		h.Logger.Infof("Writing comment %d to file: %s", c.GetID(), rawComment)
		if err := writeJSON(rawComment, c); err != nil {
			return err
		}

		comment := &Comment{
			Author: c.GetUser().GetLogin(),
			Text:   c.GetBody(),
			ID:     c.GetID(),

			Raw: rawComment,
		}
		pr.Comments = append(pr.Comments, comment)
	}

	prPath := filepath.Join(path, prFile)
	h.Logger.Infof("Writing pull request to file: %s", prPath)
	return writeJSON(prPath, pr)
}

// readJSON reads an arbitrary JSON payload from path and decodes it into the
// given interface.
func readJSON(path string, i interface{}) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return json.NewDecoder(f).Decode(i)
}

// Upload takes files stored on the filesystem and uploads new changes to
// GitHub.
func (h *GitHubHandler) Upload(ctx context.Context, path string) error {
	h.Logger.Infof("Syncing path: %s to pr %d", path, h.prNum)

	// TODO: Allow syncing from GitHub specific sources.

	prPath := filepath.Join(path, prFile)
	pr := new(PullRequest)
	if err := readJSON(prPath, pr); err != nil {
		return err
	}

	labelNames := make([]string, 0, len(pr.Labels))
	for _, l := range pr.Labels {
		labelNames = append(labelNames, l.Text)
	}
	h.Logger.Infof("Setting labels for PR %d to %v", h.prNum, labelNames)
	if _, _, err := h.Issues.ReplaceLabelsForIssue(ctx, h.owner, h.repo, h.prNum, labelNames); err != nil {
		return err
	}

	// Now sync comments.
	desiredComments := map[int64]*Comment{}
	for _, c := range pr.Comments {
		desiredComments[c.ID] = c
	}
	h.Logger.Infof("Setting comments for PR %d to: %v", h.prNum, desiredComments)

	existingComments, _, err := h.Issues.ListComments(ctx, h.owner, h.repo, h.prNum, nil)
	if err != nil {
		return err
	}

	for _, ec := range existingComments {
		dc, ok := desiredComments[ec.GetID()]
		if !ok {
			// Delete
			h.Logger.Infof("Deleting comment %d for PR %d", ec.GetID(), h.prNum)
			if _, err := h.Issues.DeleteComment(ctx, h.owner, h.repo, ec.GetID()); err != nil {
				return err
			}
		} else if dc.Text != ec.GetBody() {
			//Update
			newComment := github.IssueComment{
				Body: github.String(dc.Text),
				User: ec.User,
			}
			h.Logger.Infof("Updating comment %d for PR %d to %s", ec.GetID(), h.prNum, dc.Text)
			if _, _, err := h.Issues.EditComment(ctx, h.owner, h.repo, ec.GetID(), &newComment); err != nil {
				return err
			}
		}
		// Delete to track new comments.
		delete(desiredComments, ec.GetID())
	}

	for _, dc := range desiredComments {
		newComment := github.IssueComment{
			Body: github.String(dc.Text),
		}
		h.Logger.Infof("Creating comment %s for PR %d", dc.Text, h.prNum)
		if _, _, err := h.Issues.CreateComment(ctx, h.owner, h.repo, h.prNum, &newComment); err != nil {
			return err
		}
	}

	return nil
}
