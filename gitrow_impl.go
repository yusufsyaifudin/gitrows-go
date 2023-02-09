package gitrows

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/yusufsyaifudin/gitrows/pkg/giturl"
	"io"
	"net/url"
	"os"
	"path"
)

const gitRemoteName = "origin"

type Opt func(*DBImpl) error

func WithGitSshUrl(url string) Opt {
	return func(db *DBImpl) error {
		db.gitSshUrl = url
		return nil
	}
}

func WithPrivateKey(key []byte, password string) Opt {
	return func(db *DBImpl) error {
		db.privateKey = key
		db.privateKeyPwd = password
		return nil
	}
}

func WithBranch(branchName string) Opt {
	return func(db *DBImpl) error {
		db.gitBranch = branchName
		return nil
	}
}

func WithLocalGitVolume(dir string) Opt {
	return func(db *DBImpl) error {
		db.gitVolume = dir
		return nil
	}
}

type DBImpl struct {
	gitSshUser   string
	gitSshUrl    string
	gitURLParsed *url.URL
	gitBranch    string
	gitVolume    string

	privateKey    []byte
	privateKeyPwd string
	auth          *ssh.PublicKeys

	gitRepo *git.Repository
}

var _ DB = (*DBImpl)(nil)

func New(opts ...Opt) (*DBImpl, error) {
	db := &DBImpl{
		gitSshUser: "git",
		gitBranch:  "master",
		gitVolume:  "gitrows-data",
	}

	for _, opt := range opts {
		err := opt(db)
		if err != nil {
			return nil, err
		}
	}

	authSSH, err := ssh.NewPublicKeys(db.gitSshUser, db.privateKey, db.privateKeyPwd)
	if err != nil {
		err = fmt.Errorf("error ssh private key load: %w", err)
		return nil, err
	}

	db.gitSshUrl, err = giturl.Parse(db.gitSshUrl)
	if err != nil {
		err = fmt.Errorf("error parse git SSH url: %w", err)
		return nil, err
	}

	db.gitURLParsed, err = url.Parse(db.gitSshUrl)
	if err != nil {
		err = fmt.Errorf("error parse url.Parse git SSH url: %w", err)
		return nil, err
	}

	// git volume should reside in different path of each git repo.
	// i.e: github.com/yusufsyaifudin/common-dev-config
	//      -> should create tree directory ${db.gitVolume}/github.com/yusufsyaifudin/common-dev-config
	db.gitVolume = fmt.Sprintf("%s/%s/%s", db.gitVolume, db.gitURLParsed.Host, db.gitURLParsed.Path)

	db.auth = authSSH

	return db, nil
}

// gitClone is like `git reset --hard HEAD && git pull` command,
// where remove all uncommitted changes and then pull.
// This to make sure that every command in current local git is up-to-date
// with the origin (remote) git repository.
func (db *DBImpl) gitClone(ctx context.Context) (err error) {
	cloneOpt := &git.CloneOptions{
		URL:        db.gitSshUrl,
		Auth:       db.auth,
		RemoteName: gitRemoteName,
		// Only fetch specific branch because we don't want all git histories eats up our RAM
		ReferenceName: plumbing.NewBranchReferenceName(db.gitBranch),
		SingleBranch:  true, // Fetch only ReferenceName if true.
		NoCheckout:    true,
		Depth:         1,         // fetch only depth 1
		Progress:      os.Stdout, // TODO: use another logging options
	}

	// git clone <url> --depth 1 --branch <branch> --single-branch
	// eg: git clone git@github.com:go-git/go-git.git --depth 1 --branch master --single-branch
	db.gitRepo, err = git.PlainCloneContext(ctx, db.gitVolume, false, cloneOpt)
	if errors.Is(err, git.ErrRepositoryAlreadyExists) {
		err = nil // discard error caused by ErrRepositoryAlreadyExists
	}

	if errors.Is(err, transport.ErrEmptyRemoteRepository) {
		// discard error and create new repo here
		err = nil
		db.gitRepo, err = git.PlainInit(db.gitVolume, false)
		if err != nil {
			err = fmt.Errorf("cannot `git init` new repository: %w", err)
			return
		}
	}

	if err != nil {
		err = fmt.Errorf("clone repository %s error: %w", db.gitSshUrl, err)
		return
	}

	// always open the local repository
	db.gitRepo, err = git.PlainOpenWithOptions(db.gitVolume, &git.PlainOpenOptions{
		DetectDotGit:          true,
		EnableDotGitCommonDir: true,
	})
	if err != nil {
		err = fmt.Errorf("open local repository %s error: %w", db.gitSshUrl, err)
		return
	}

	return
}

func (db *DBImpl) gitFetch(ctx context.Context) (err error) {
	// check remote existence
	// ex: git remote get-url origin
	var remote *git.Remote
	remote, err = db.gitRepo.Remote(gitRemoteName)
	if errors.Is(err, git.ErrRemoteNotFound) {
		// discard if remote not found, we will try to create if remote is still nil
		err = nil
	}

	if err != nil {
		err = fmt.Errorf("cannot get remote '%s': %w", gitRemoteName, err)
		return
	}

	// if still nil, then try to add
	// ex: git remote add origin <git-url>
	if remote == nil {
		remote, err = db.gitRepo.CreateRemote(&config.RemoteConfig{
			Name: gitRemoteName,
			URLs: []string{
				db.gitSshUrl,
			},
		})

		if err != nil {
			err = fmt.Errorf("cannot `git remote add %s %s`: %w", gitRemoteName, db.gitSshUrl, err)
			return
		}
	}

	// if still nil, then return error
	if remote == nil {
		err = fmt.Errorf("nil remote after `git remote add %s %s`", gitRemoteName, db.gitSshUrl)
		return
	}

	// git fetch <remote> <remote branch>:<local branch>
	// ex: git fetch origin master:master --depth 1
	branchName := plumbing.NewBranchReferenceName(db.gitBranch)
	refSpec := fmt.Sprintf("%s:%s", branchName, branchName)
	err = remote.FetchContext(ctx, &git.FetchOptions{
		RemoteName: gitRemoteName,
		RefSpecs: []config.RefSpec{
			config.RefSpec(refSpec),
		},
		Depth:    1,
		Auth:     db.auth,
		Progress: os.Stdout,
		Force:    true,
	})

	if errors.Is(err, git.NoErrAlreadyUpToDate) {
		err = nil // discard error when contain "already up-to-date" warning
	}

	if errors.Is(err, transport.ErrEmptyRemoteRepository) {
		// create local branch on if remote repository doesn't have this branch
		// similar like: git checkout --orphan <branch-name>
		// https://github.com/go-git/go-git/pull/439#discussion_r908421596
		ref := plumbing.NewSymbolicReference(plumbing.HEAD, branchName)
		err = db.gitRepo.Storer.SetReference(ref)
		if err != nil {
			err = fmt.Errorf("cannot set reference %s: %w", ref, err)
			return
		}

		return // early return when set reference done
	}

	if err != nil {
		err = fmt.Errorf("cannot `git fetch %s %s --depth 1`: %w", gitRemoteName, refSpec, err)
		return
	}

	return
}

func (db *DBImpl) gitCheckout(ctx context.Context) (err error) {
	var worktree *git.Worktree
	worktree, err = db.gitRepo.Worktree()
	if err != nil {
		err = fmt.Errorf("cannot get worktree of local git repo: %w", err)
		return
	}

	// always do `git reset --hard` to ensure that we don't create any changes on read only repository
	canCheckout := true
	err = worktree.Reset(&git.ResetOptions{
		Mode: git.HardReset,
	})
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		err = nil
		canCheckout = false
	}

	if err != nil {
		err = fmt.Errorf("cannot hard reset on local git repo: %w", err)
		return
	}

	// Git branch will not be exists until it committed.
	// In case we Clone empty git repository, mean that it doesn't have any branches.
	// So, we only be able to check out when the branch exists.
	// https://stackoverflow.com/a/21769914/5489910
	// git checkout <local branch>
	// ex: git checkout master
	if canCheckout {
		checkoutBranch := plumbing.NewBranchReferenceName(db.gitBranch)
		err = worktree.Checkout(&git.CheckoutOptions{
			Branch: checkoutBranch,
			Force:  true,
		})
		if err != nil {
			err = fmt.Errorf("cannot checkout on local git: `git checkout -b %s` : %w", checkoutBranch, err)
			return
		}

		return
	}

	// When the git checkout cannot be done because of `canCheckout` false.
	// We can be rest assured that the current reference is in the branch name specified in the config.
	// Because in the process gitFetch we already try to SetReference(ref) to the HEAD.
	return
}

func (db *DBImpl) forcePull(ctx context.Context) (err error) {
	err = db.gitClone(ctx)
	if err != nil {
		err = fmt.Errorf("git clone error: %w", err)
		return
	}

	err = db.gitFetch(ctx)
	if err != nil {
		err = fmt.Errorf("git fetch error: %w", err)
		return
	}

	err = db.gitCheckout(ctx)
	if err != nil {
		err = fmt.Errorf("git checkout error: %w", err)
		return
	}

	return
}

func (db *DBImpl) Get(ctx context.Context, key string) (data []byte, err error) {
	key = path.Clean(key)

	err = db.forcePull(ctx)
	if err != nil {
		err = fmt.Errorf("get command: %w", err)
		return
	}

	worktree, err := db.gitRepo.Worktree()
	if err != nil {
		err = fmt.Errorf("get command: cannot get worktree: %w", err)
		return
	}

	fs := worktree.Filesystem

	matchFile, err := fs.OpenFile(key, os.O_RDONLY, os.ModePerm)
	defer func() {
		if matchFile == nil {
			return
		}

		if _err := matchFile.Close(); _err != nil {
			err = fmt.Errorf("get command: failed to close file: %w", _err)
			return
		}
	}()

	if err != nil {
		err = fmt.Errorf("get command: cannot open file: %w", err)
		return
	}

	// protects file for access from another process
	err = matchFile.Lock()
	if err != nil {
		err = fmt.Errorf(
			"get command: cannot acquire file lock on key '%s' to protects against access from other processes: %w",
			key, err,
		)
		return
	}

	defer func() {
		if matchFile == nil {
			return
		}

		if _err := matchFile.Unlock(); _err != nil {
			err = fmt.Errorf("get command: failed to unlock file '%s': %w", key, _err)
			return
		}
	}()

	buf := &bytes.Buffer{}
	_, err = buf.ReadFrom(matchFile)
	if err != nil {
		err = fmt.Errorf("get command: cannot read file buffer: %w", err)
		return
	}

	data = buf.Bytes()
	buf.Reset()
	return
}

// writeFile Will open the file (and create if not exist), then write data into it.
// It will lock the file when opened, to ensure that no other process will read-write the same file.
// After writing file, this function also do `git add` command.
// Then it will return worktree, so later we can commit it (at once) and then pushed.
func (db *DBImpl) writeFile(ctx context.Context, key string, data []byte, mode string) (worktree *git.Worktree, err error) {
	key = path.Clean(key)

	worktree, err = db.gitRepo.Worktree()
	if err != nil {
		err = fmt.Errorf("cannot get worktree: %w", err)
		return
	}

	if worktree == nil {
		err = fmt.Errorf("worktree is nil")
		return
	}

	fs := worktree.Filesystem

	var fileInfo os.FileInfo
	var fileInfoErr error
	fileInfo, fileInfoErr = fs.Stat(key)
	if os.IsNotExist(fileInfoErr) {
		// if file not exist, then we can do CREATE and UPSERT
		// fileInfo probably nil here
		fileInfoErr = nil
	}

	if fileInfoErr != nil {
		err = fmt.Errorf("cannot create '%s' because os.Stat is error: %w", key, fileInfoErr)
		return
	}

	switch mode {
	case "CREATE":
		if fileInfo != nil {
			err = fmt.Errorf("%w: cannot create '%s' because os.Stat said that key is exist", os.ErrExist, key)
			return
		}

	case "UPSERT":
		// do nothing, since UPSERT can CREATE if file not exist or UPDATE if exist
		if fileInfo != nil && fileInfo.IsDir() {
			err = fmt.Errorf("cannot upsert '%s' because os.Stat said that key is a directory", key)
			return
		}

	default:
		err = fmt.Errorf("unknown write file mode=%s", mode)
		return
	}

	// Open file with mode Create if not exist, and Append if exist.
	var matchFile billy.File
	matchFile, err = fs.OpenFile(key, os.O_APPEND|os.O_CREATE|os.O_RDWR, os.ModePerm)
	defer func() {
		if matchFile == nil {
			return
		}

		if _err := matchFile.Close(); _err != nil {
			err = fmt.Errorf("failed to close file: %w", err)
			return
		}
	}()

	if err != nil {
		err = fmt.Errorf("cannot open file: %w", err)
		return
	}

	// protects file for access from another process
	err = matchFile.Lock()
	if err != nil {
		err = fmt.Errorf(
			"cannot acquire file lock on key '%s' to protects against access from other processes: %w",
			key, err,
		)
		return
	}

	defer func() {
		if matchFile == nil {
			return
		}

		if _err := matchFile.Unlock(); _err != nil {
			err = fmt.Errorf("failed to unlock file '%s': %w", key, _err)
			return
		}
	}()

	// always truncate, even on the mode CREATE, because in mode create everything has to be starts from empty file.
	err = matchFile.Truncate(0)
	if err != nil {
		err = fmt.Errorf("failed to shrink the file: %w", err)
		return
	}

	_, err = matchFile.Write(data)
	if err != nil {
		err = fmt.Errorf("cannot write file buffer: %w", err)
		return
	}

	_, err = worktree.Add(key)
	if err != nil {
		err = fmt.Errorf("cannot `git add %s`: %w", key, err)
		return
	}

	return
}

func (db *DBImpl) Create(ctx context.Context, key string, data []byte, opts ...CreateOpt) (commitHashString string, err error) {
	cfg := &CreateConfig{
		commitMsg: "gitrows: CREATE",
	}

	for _, opt := range opts {
		err = opt(cfg)
		if err != nil {
			err = fmt.Errorf("create command: %w", err)
			return
		}
	}

	key = path.Clean(key)
	err = db.forcePull(ctx)
	if err != nil {
		err = fmt.Errorf("create command: %w", err)
		return
	}

	worktree, err := db.writeFile(ctx, key, data, "CREATE")
	if err != nil {
		err = fmt.Errorf("create command: %w", err)
		return
	}

	var commitHash plumbing.Hash
	commitMsg := cfg.commitMsg
	commitHash, err = worktree.Commit(commitMsg, &git.CommitOptions{
		All:               true,
		AllowEmptyCommits: false,
	})
	if err != nil {
		err = fmt.Errorf("create command: cannot `git commit -m %q`: %w", commitMsg, err)
		return
	}

	commitHashString = commitHash.String()

	refSpec := fmt.Sprintf("%s:%s", plumbing.NewBranchReferenceName(db.gitBranch), plumbing.NewBranchReferenceName(db.gitBranch))
	err = db.gitRepo.PushContext(ctx, &git.PushOptions{
		RemoteName: gitRemoteName,
		RefSpecs: []config.RefSpec{
			config.RefSpec(refSpec),
		},
		Auth:     db.auth,
		Progress: os.Stdout,
		Force:    true,
		Atomic:   true,
	})

	if err != nil {
		err = fmt.Errorf("create command: cannot `git push -f %s`: %w", refSpec, err)
		return
	}

	return
}

func (db *DBImpl) Upsert(ctx context.Context, key string, data []byte, opts ...UpsertOpt) (commitHashString string, changed bool, err error) {
	cfg := &UpsertConfig{
		commitMsg:        "gitrows: UPSERT",
		allowEmptyCommit: false,
	}

	for _, opt := range opts {
		err = opt(cfg)
		if err != nil {
			err = fmt.Errorf("upsert command: %w", err)
			return
		}
	}

	key = path.Clean(key)
	err = db.forcePull(ctx)
	if err != nil {
		err = fmt.Errorf("upsert command: %w", err)
		return
	}

	worktree, err := db.writeFile(ctx, key, data, "UPSERT")
	if err != nil {
		err = fmt.Errorf("upsert command: %w", err)
		return
	}

	worktreeStatus, err := worktree.Status()
	if err != nil {
		err = fmt.Errorf("upsert command: cannot `git status`: %w", err)
		return
	}

	changed = !worktreeStatus.IsClean()

	// if allow empty commit false, and no file is changed in worktree, then skip it
	if !cfg.allowEmptyCommit && !changed {
		var head *plumbing.Reference
		head, err = db.gitRepo.Head()
		if err != nil {
			err = fmt.Errorf("upsert command: cannot get HEAD reference when --allow-empty and git status both false: %w", err)
			return
		}

		// TODO: must return last commit Hash when this file is changed
		commitHashString = head.Hash().String()
		return
	}

	var commitHash plumbing.Hash
	commitMsg := cfg.commitMsg
	commitHash, err = worktree.Commit(commitMsg, &git.CommitOptions{
		All:               true,
		AllowEmptyCommits: cfg.allowEmptyCommit,
	})
	if err != nil {
		err = fmt.Errorf("upsert command: cannot `git commit -m %q`: %w", commitMsg, err)
		return
	}

	// using current commit as return
	commitHashString = commitHash.String()

	refSpec := fmt.Sprintf("%s:%s", plumbing.NewBranchReferenceName(db.gitBranch), plumbing.NewBranchReferenceName(db.gitBranch))
	err = db.gitRepo.PushContext(ctx, &git.PushOptions{
		RemoteName: gitRemoteName,
		RefSpecs: []config.RefSpec{
			config.RefSpec(refSpec),
		},
		Auth:     db.auth,
		Progress: os.Stdout,
		Force:    true,
		Atomic:   true,
	})

	if err != nil {
		err = fmt.Errorf("upsert command: cannot `git push -f %s`: %w", refSpec, err)
		return
	}

	return
}

func (db *DBImpl) Delete(ctx context.Context, key string, opts ...DeleteOpt) (commitHashString string, err error) {
	cfg := &DeleteConfig{
		commitMsg: "gitrows: DELETE",
	}

	for _, opt := range opts {
		err = opt(cfg)
		if err != nil {
			err = fmt.Errorf("delete command: %w", err)
			return
		}
	}

	key = path.Clean(key)
	err = db.forcePull(ctx)
	if err != nil {
		err = fmt.Errorf("delete command: %w", err)
		return
	}

	worktree, err := db.gitRepo.Worktree()
	if err != nil {
		err = fmt.Errorf("delete command: cannot get worktree: %w", err)
		return
	}

	fs := worktree.Filesystem

	// if file not exist then error
	err = fs.Remove(key)
	if err != nil {
		err = fmt.Errorf("delete command: cannot delete '%s': %w", key, err)
		return
	}

	_, err = worktree.Add(key)
	if err != nil {
		err = fmt.Errorf("delete command: cannot `git add %s`: %w", key, err)
		return
	}

	var commitHash plumbing.Hash
	commitMsg := cfg.commitMsg
	commitHash, err = worktree.Commit(commitMsg, &git.CommitOptions{
		All:               true,
		AllowEmptyCommits: false,
	})
	if err != nil {
		err = fmt.Errorf("delete command: cannot `git commit -m %q`: %w", commitMsg, err)
		return
	}

	commitHashString = commitHash.String()

	refSpec := fmt.Sprintf("%s:%s", plumbing.NewBranchReferenceName(db.gitBranch), plumbing.NewBranchReferenceName(db.gitBranch))
	err = db.gitRepo.PushContext(ctx, &git.PushOptions{
		RemoteName: gitRemoteName,
		RefSpecs: []config.RefSpec{
			config.RefSpec(refSpec),
		},
		Auth:     db.auth,
		Progress: os.Stdout,
		Force:    true,
		Atomic:   true,
	})

	if err != nil {
		err = fmt.Errorf("delete command: cannot `git push -f %s`: %w", refSpec, err)
		return
	}

	return
}

type kvIter struct {
	k          string
	v          func() (io.ReadCloser, error)
	lastCommit *object.Commit
}

func (k *kvIter) Key() string {
	return k.k
}

func (k *kvIter) Value() (io.ReadCloser, error) {
	return k.v()
}

func (k *kvIter) LastCommit() string {
	if k.lastCommit == nil {
		return ""
	}
	return k.lastCommit.Hash.String()
}

var _ KV = (*kvIter)(nil)

type entriesImpl struct {
	kvs []KV
}

var _ Entries = (*entriesImpl)(nil)

func (e *entriesImpl) KVs() []KV {
	return e.kvs
}

// List fetch all files in current git repository.
// This is similar like command: git ls-tree <branch-name> --name-only
//
// Please note, that since we only `git fetch` with depth 1, then the LastCommit may return only the last commit
// that exist in the local repository.
// If you want to ensure that the LastCommit() return the last commit hash when the file is modified,
// then you MUST `git fetch` or `git pull` all Git history.
// This because, when you have 1000 commits, but 1 file is never changed after first commit,
// then in order to track the "first commit" we need all parent commit history
// which only be available when we `git fetch` all history.
func (db *DBImpl) List(ctx context.Context, opts ...ListOpt) (entries Entries, err error) {
	cfg := &ListConfig{}

	for _, opt := range opts {
		err = opt(cfg)
		if err != nil {
			err = fmt.Errorf("list command: %w", err)
			return
		}
	}

	err = db.forcePull(ctx)
	if err != nil {
		err = fmt.Errorf("list command: %w", err)
		return
	}

	// get reference of branch
	branchName := plumbing.NewBranchReferenceName(db.gitBranch)
	ref, err := db.gitRepo.Reference(branchName, false)
	if err != nil {
		err = fmt.Errorf("list command: retrieving ref for branch %s error: %w", branchName, err)
		return
	}

	// get all files of current branch, this similar like git ls-tree -r <branch-name>
	commit, err := db.gitRepo.CommitObject(ref.Hash())
	if err != nil {
		err = fmt.Errorf("list command: retrieving the commit object of branch %s error: %w", branchName, err)
		return
	}

	tree, err := commit.Tree()
	if err != nil {
		err = fmt.Errorf("list command: retrieve the tree from the commit %s (%s) error: %w", commit.ID(), branchName, err)
		return
	}

	paths := make([]string, 0)
	kvIters := make([]*kvIter, 0)
	err = tree.Files().ForEach(func(file *object.File) error {
		// when filter applied
		if path.Clean(cfg.prefix) != "" && path.Dir(file.Name) == cfg.prefix {
			paths = append(paths, file.Name)
			kvIters = append(kvIters, &kvIter{
				k: file.Name,
				v: file.Reader,
			})
			return nil
		}

		paths = append(paths, file.Name)
		kvIters = append(kvIters, &kvIter{
			k: file.Name,
			v: file.Reader,
		})
		return nil
	})
	if err != nil {
		err = fmt.Errorf("list command: cannot iterate tree: %w", err)
		return
	}

	worktree, err := db.gitRepo.Worktree()
	if err != nil {
		err = fmt.Errorf("list command: cannot get worktree: %w", err)
		return
	}

	fs := worktree.Filesystem

	commitNodeIndex := getCommitNodeIndex(db.gitRepo, fs)
	commitNode, err := commitNodeIndex.Get(ref.Hash())
	if err != nil {
		err = fmt.Errorf("list command: cannot get commit node index: %w", err)
		return
	}

	revs, err := getLastCommitForPaths(commitNode, paths)
	if err != nil {
		err = fmt.Errorf("list command: cannot get last commit for paths: %w", err)
		return
	}

	// build output using interface implementation
	entryRow := make([]KV, 0)
	for _, kv := range kvIters {
		kv.lastCommit = commit // use current commit as default

		lastCommit, exist := revs[kv.Key()]
		if exist && lastCommit != nil {
			kv.lastCommit = lastCommit
		}

		entryRow = append(entryRow, kv)
	}

	entries = &entriesImpl{
		kvs: entryRow,
	}
	return
}
