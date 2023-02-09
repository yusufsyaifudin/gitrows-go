package giturl

import (
	"errors"
	"net/url"
	"regexp"
)

// Mostly of this code is modified from Go internal package
// https://github.com/golang/go/blob/go1.18.4/src/cmd/go/internal/vcs/vcs.go#L257-L301

// scpSyntaxRe matches the SCP-like addresses used by Git to access
// repositories by SSH.
var scpSyntaxRe = regexp.MustCompile(`^([a-zA-Z0-9_]+)@([a-zA-Z0-9._-]+):(.*)$`)

func Parse(str string) (remoteRepo string, err error) {
	var repoURL *url.URL
	if m := scpSyntaxRe.FindStringSubmatch(str); len(m) >= 4 {
		// Match SCP-like syntax and convert it to a URL.
		// Eg, "git@github.com:user/repo" becomes
		// "ssh://git@github.com/user/repo".
		repoURL = &url.URL{
			Scheme: "ssh",
			User:   url.User(m[1]),
			Host:   m[2],
			Path:   m[3],
		}
	} else {
		repoURL, err = url.Parse(str)
		if err != nil {
			return "", err
		}
	}

	// https://github.com/golang/go/blob/88a06f40dfcdc4d37346be169f2b1b9070f38bb3/src/cmd/go/internal/vcs/vcs.go#L243
	vcsGitScheme := []string{"git", "https", "http", "git+ssh", "ssh"}

	// Iterate over insecure schemes too, because this function simply
	// reports the state of the repo. If we can't see insecure schemes then
	// we can't report the actual repo URL.
	for _, s := range vcsGitScheme {
		if repoURL.Scheme == s {
			return repoURL.String(), nil
		}
	}
	return "", errors.New("unable to parse output of git url: " + str)
}
