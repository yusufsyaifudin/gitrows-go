package giturl_test

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yusufsyaifudin/gitrows/pkg/giturl"
)

func TestParse(t *testing.T) {
	tests := []struct {
		path       string
		remoteRepo string
	}{
		{
			path:       "git@github.com:yusufsyaifudin/common-dev-config.git",
			remoteRepo: "ssh://git@github.com/yusufsyaifudin/common-dev-config.git",
		},
		{
			path:       "ssh://git@github.com/yusufsyaifudin/common-dev-config.git",
			remoteRepo: "ssh://git@github.com/yusufsyaifudin/common-dev-config.git",
		},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			remoteRepo, err := giturl.Parse(test.path)
			assert.Equal(t, test.remoteRepo, remoteRepo)
			assert.NoError(t, err)

			u, err := url.Parse(remoteRepo)
			assert.NotNil(t, u)
			assert.NoError(t, err)
		})
	}

}
