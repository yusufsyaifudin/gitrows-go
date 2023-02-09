# gitrows-go

Git as Key-Value store. Use this project as Golang library, see [Installation](#Installation). 


> WARNING!
>
> Since process Create, Update and Delete need to be pushed to Remote Repository,
> there is a chance that `git push` process is failed when you have concurrent `git push` process
> (because your current local is not descent from remote repository).
>
> To mitigate this, you need distributed lock when using this repository.
>
> Distributed lock makes your performance slower, but this library is not intended to save the hot data,
> instead you can use this library to share with your team configuration, etc.

## Background

Git has it own database to store object. If you ever to look at your Git project, you may see the hidden directory `.git` which stores your data and track it changes.

You can read more about Git database internal here:
* https://github.blog/2022-08-29-gits-database-internals-i-packed-object-store/
* https://git-scm.com/book/en/v2/Git-Internals-Git-Objects

Then, what good about Git is it has:

* "distributed database", mean if you `git push` your local Git repository, 
  the remote repository must descent from it, otherwise your `git push` command will fail (except you `--force` it).
* we can see (and track) the history of our changes, so we can have an "audit logs" out of the box.
  This is similar when you do only INSERT command to your database, or using append-only logs model in your system.

With its great features, some folks inspired to create a Database that similar to Git:
* https://github.com/dolthub/dolt
* https://www.dolthub.com/blog/2021-11-26-so-you-want-git-database/


Some another folks, try to use current Git "database" into NoSQL database:

* [Gitrows](https://gitrows.com/) for json and csv files.
* [Kenneth Truyers](https://www.kenneth-truyers.net/2016/10/13/git-nosql-database/)
* [nede.dev](https://nede.dev/blog/turning-git-into-an-application-database)

### So what is this?

Unlike other mentioned project, this `gitrows-go` attempt to make current Git repository as a Key-Value database.
This is mean that if you have current repository in Github, Gitlab, Bitbucket, or any other Git repository provider,
you can make it as Key-Value database.

## Installation

```shell
go get -u github.com/yusufsyaifudin/gitrows-go
```

Use import path: `github.com/yusufsyaifudin/gitrows`


### Example

Here's the basic example where can Upsert and Get the content with key `note.md`.

```go
package main

import (
   "fmt"
   "log"
   "context"
   "encoding/base64"
   
   "github.com/yusufsyaifudin/gitrows"
)

func main() {
   const PRIVATE_KEY_B64 = ""
   privateKey, err := base64.StdEncoding.DecodeString(PRIVATE_KEY_B64)
   checkError(err)

   db, err := gitrows.New(
      gitrows.WithGitSshUrl("git@github.com:yusufsyaifudin/gitrows-test-repo.git"),
      gitrows.WithPrivateKey(privateKey, ""),
      gitrows.WithBranch("gitrows"),
   )
   checkError(err)

   ctx := context.TODO()
   path := "note.md"

   hash, changed, err := db.Upsert(ctx, path, []byte("rewrite all"),
      gitrows.UpsertCommitMsg("my update"),
      gitrows.UpsertAllowEmptyCommit(false),
   )
   checkError(err)
   
   fmt.Printf("Upsert is success=%T with commit hash=%q\n", changed, hash)
   

   dataGet, err := db.Get(ctx, path)
   checkError(err)
   fmt.Printf("The value of key=%q:\n%s\n", path, dataGet)
}

func checkError(err error) {
   if err != nil {
      log.Fatal(err)
   }
}

```

## What processes behind this?

When you `Get`, `Create`, `Upsert`, and `Delete` the `key`, we will try to:

1. Clone the project from the specified Git remote repository into local directory.
   That's why you need to set directory name using `WithLocalGitVolume`, this is to clone and operate local Git repository.
   If you're deploying inside Container environment (Docker and Kubernetes), you doesn't require to
   mount the [volume into Host](https://docs.docker.com/storage/volumes/) or
   use [`PersistentVolume`](https://kubernetes.io/docs/concepts/storage/persistent-volumes/).
   This is because if the Container is restarted and the local repository is missing,
   this library will always try to Git clone from remote repository to get the update.
   If the repository already exist in the local directory, then we will try to `git fetch` it.

2. After clone, then we try to add remote repository URL using `git remote add origin <git-ssh-url>`.
   We always use `origin` as the remote name, and SSH URL as the Git address.
   If `origin` is already exist, we skipp `git remote add` process.

3. After adding the `git remote`, we try `git fetch origin <remote-branch>:<local-branch> --depth 1`.
   a. If Branch name is not exist, we will create the branch in local Git repo: `git checkout --orphan <branch-name>`

4. Then we try to `git checkout <branch-name>` if after `git fetch` we found the `<branch-name>` from remote repository.

5. Done, after all steps above, we already have local repository updated and synced with remote repository.
   Then we can do any command here (Create, Read, Update, Delete).

## Use-case

Some example use-case that you can do with this library are:

* Fetch in cronjob to see changes on specific files in Git. (Similar like what ArgoCD do when listening directory changes https://github.com/argoproj/argo-cd/tree/v2.6.1/util/git)
