package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/google/go-github/github"
	"github.com/gorilla/mux"
)

// key defines keys for associating data to PRs/issues in the fake server.
type key struct {
	owner string
	repo  string
	id    int64
}

// FakeGitHub is a fake GitHub server for use in tests.
type FakeGitHub struct {
	*mux.Router

	pr       map[key]*github.PullRequest
	comments map[key][]*github.IssueComment
}

// NewFakeGitHub returns a new FakeGitHub.
func NewFakeGitHub() *FakeGitHub {
	s := &FakeGitHub{
		Router:   mux.NewRouter(),
		pr:       make(map[key]*github.PullRequest),
		comments: make(map[key][]*github.IssueComment),
	}
	s.HandleFunc("/repos/{owner}/{repo}/pulls/{number}", s.getPullRequest).Methods(http.MethodGet)
	s.HandleFunc("/repos/{owner}/{repo}/issues/{number}/comments", s.getComments).Methods(http.MethodGet)
	s.HandleFunc("/repos/{owner}/{repo}/issues/{number}/comments", s.createComment).Methods(http.MethodPost)
	s.HandleFunc("/repos/{owner}/{repo}/issues/{number}/labels", s.updateLabels).Methods(http.MethodPut)

	return s
}

func prKey(r *http.Request) (key, error) {
	pr, err := strconv.ParseInt(mux.Vars(r)["number"], 10, 64)
	if err != nil {
		return key{}, err
	}
	return key{
		owner: mux.Vars(r)["owner"],
		repo:  mux.Vars(r)["repo"],
		id:    pr,
	}, nil
}

// AddPullRequest adds the given pull request to the fake GitHub server.
func (g *FakeGitHub) AddPullRequest(pr *github.PullRequest) {
	key := key{
		owner: pr.GetBase().GetUser().GetLogin(),
		repo:  pr.GetBase().GetRepo().GetName(),
		id:    pr.GetID(),
	}
	g.pr[key] = pr
}

// AddComment adds a comment to the fake GitHub server.
func (g *FakeGitHub) AddComment(owner string, repo string, pr int64, comment *github.IssueComment) {
	key := key{
		owner: owner,
		repo:  repo,
		id:    pr,
	}
	c := g.comments[key]
	if c == nil {
		c = []*github.IssueComment{comment}
	} else {
		c = append(c, comment)
	}
	g.comments[key] = c
}

func (g *FakeGitHub) getPullRequest(w http.ResponseWriter, r *http.Request) {
	key, err := prKey(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}

	pr, ok := g.pr[key]
	if !ok {
		http.Error(w, fmt.Sprintf("%v not found", key), http.StatusNotFound)
		return
	}

	if err := json.NewEncoder(w).Encode(pr); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (g *FakeGitHub) getComments(w http.ResponseWriter, r *http.Request) {
	key, err := prKey(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}

	comments, ok := g.comments[key]
	if !ok {
		comments = []*github.IssueComment{}
	}
	if err := json.NewEncoder(w).Encode(comments); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (g *FakeGitHub) createComment(w http.ResponseWriter, r *http.Request) {
	key, err := prKey(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}

	c := new(github.IssueComment)
	if err := json.NewDecoder(r.Body).Decode(c); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	comments, ok := g.comments[key]
	if !ok {
		comments = []*github.IssueComment{}
	}
	c.ID = github.Int64(int64(len(comments) + 1))
	g.comments[key] = append(comments, c)

	w.WriteHeader(http.StatusOK)
}

func (g *FakeGitHub) updateLabels(w http.ResponseWriter, r *http.Request) {
	key, err := prKey(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}

	pr, ok := g.pr[key]
	if !ok {
		http.Error(w, "pull request not found", http.StatusNotFound)
		return
	}

	payload := []string{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	pr.Labels = make([]*github.Label, 0, len(payload))
	for _, l := range payload {
		pr.Labels = append(pr.Labels, &github.Label{
			Name: github.String(l),
		})
	}

	w.WriteHeader(http.StatusOK)
}
