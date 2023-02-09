package gitrows_test

import (
	"context"
	"encoding/base64"
	"github.com/caarlos0/env"
	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
	"github.com/yusufsyaifudin/gitrows"
	"testing"
)

type IntegrationTestConfig struct {
	IntegrationTestEnable bool   `env:"INTEGRATION_TEST_ENABLE"`
	PrivateKey            string `env:"PRIVATE_KEY_BASE64"`
}

func TestIntegration(t *testing.T) {
	err := godotenv.Load(".env.test_integration")
	assert.NoError(t, err)

	cfg := &IntegrationTestConfig{}
	err = env.Parse(cfg)
	assert.NoError(t, err)

	if !cfg.IntegrationTestEnable {
		t.Skip("skipped TestIntegration because INTEGRATION_TEST_ENABLE=", cfg.IntegrationTestEnable)
	}

	privateKey, err := base64.StdEncoding.DecodeString(cfg.PrivateKey)
	assert.NotEmpty(t, privateKey)
	assert.NoError(t, err)

	db, err := gitrows.New(
		gitrows.WithGitSshUrl("git@github.com:yusufsyaifudin/gitrows-test-repo.git"),
		gitrows.WithPrivateKey(privateKey, ""),
		gitrows.WithBranch("gitrows"),
	)
	assert.NotNil(t, db)
	assert.NoError(t, err)

	ctx := context.TODO()
	path := "note.md"

	hash, changed, err := db.Upsert(ctx, path, []byte("rewrite all"),
		gitrows.UpsertCommitMsg("my update"),
		gitrows.UpsertAllowEmptyCommit(false),
	)
	assert.NotEmpty(t, hash)
	assert.False(t, changed)
	assert.NoError(t, err)

	dataGet, err := db.Get(ctx, path)
	t.Logf("Key='%s' content: \n%s\n", path, dataGet)
	assert.NotNil(t, dataGet)
	assert.NoError(t, err)
}
