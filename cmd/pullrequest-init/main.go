/*
Copyright 2018 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package main

import (
	"context"
	"flag"

	"github.com/knative/pkg/logging"
)

const (
	prFile = "pr.json"
)

var (
	prURL = flag.String("url", "", "The url of the pull request to initialize.")
	path  = flag.String("path", "", "Path of directory under which PR will be copied")
	mode  = flag.String("mode", "download", "Whether to operate in download or upload mode")
)

// PullRequest represents a generic pull request resource.
type PullRequest struct {
	Type       string
	ID         int64
	Head, Base *GitReference
	Comments   []*Comment
	Labels     []*Label

	Raw string
}

// GitReference represents a git ref object. See
// https://git-scm.com/book/en/v2/Git-Internals-Git-References for more details.
type GitReference struct {
	Repo   string
	Branch string
	SHA    string
}

// Comment represents a pull request comment.
type Comment struct {
	Text string

	// Output only.

	Author string
	ID     int64
	Raw    string
}

// Label represents a Pull Request Label
type Label struct {
	Text string
}

func main() {
	flag.Parse()
	logger, _ := logging.NewLogger("", "pullrequest-init")
	defer logger.Sync()
	ctx := context.Background()

	client, err := NewGitHubHandler(ctx, logger, *prURL)
	if err != nil {
		logger.Fatalf("error creating GitHub client: %v", err)
	}

	switch *mode {
	case "download":
		logger.Info("RUNNING DOWNLOAD!")
		if err := client.Download(ctx, *path); err != nil {
			logger.Fatal(err)
		}
	case "upload":
		logger.Info("RUNNING UPLOAD!")
		if err := client.Upload(ctx, *path); err != nil {
			logger.Fatal(err)
		}
	}
}
