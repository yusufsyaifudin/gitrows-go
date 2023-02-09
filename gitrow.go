package gitrows

import (
	"context"
	"strings"
)

type DB interface {
	Get(ctx context.Context, key string) (data []byte, err error)
	Create(ctx context.Context, key string, data []byte, opts ...CreateOpt) (commitHashString string, err error)
	Upsert(ctx context.Context, key string, data []byte, opts ...UpsertOpt) (commitHashString string, changed bool, err error)
	Delete(ctx context.Context, key string, opts ...DeleteOpt) (commitHashString string, err error)
}

type CreateOpt func(*CreateConfig) error

type CreateConfig struct {
	commitMsg string
}

func CreateCommitMsg(msg string) CreateOpt {
	return func(config *CreateConfig) error {
		msg = strings.TrimSpace(msg)
		if msg == "" {
			return nil
		}

		config.commitMsg = msg
		return nil
	}
}

type UpsertOpt func(*UpsertConfig) error

type UpsertConfig struct {
	commitMsg        string
	allowEmptyCommit bool
}

func UpsertCommitMsg(msg string) UpsertOpt {
	return func(config *UpsertConfig) error {
		msg = strings.TrimSpace(msg)
		if msg == "" {
			return nil
		}

		config.commitMsg = msg
		return nil
	}
}

// UpsertAllowEmptyCommit enable empty commits to be created. An empty commit
// is when no changes to the tree were made, but a new commit message is
// provided. The default behavior is false, which results in ErrEmptyCommit.
func UpsertAllowEmptyCommit(b bool) UpsertOpt {
	return func(config *UpsertConfig) error {
		config.allowEmptyCommit = b
		return nil
	}
}

type DeleteOpt func(*DeleteConfig) error

type DeleteConfig struct {
	commitMsg string
}

func DeleteCommitMsg(msg string) DeleteOpt {
	return func(config *DeleteConfig) error {
		msg = strings.TrimSpace(msg)
		if msg == "" {
			return nil
		}

		config.commitMsg = msg
		return nil
	}
}
