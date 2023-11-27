package actions

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/fluxcd/pkg/gittestserver"
	"github.com/go-git/go-billy/v5/osfs"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/filesystem"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	gitServer *gittestserver.GitServer
)

func TestPlaybookActions(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Playbook action suite")
}

var _ = ginkgo.BeforeSuite(func() {
	var err error
	gitServer, err = gittestserver.NewTempGitServer()
	Expect(err).NotTo(HaveOccurred())

	logger.Infof("%v", gitServer.Root())

	go func() {
		defer ginkgo.GinkgoRecover() // Required by ginkgo, if an assertion is made in a goroutine.
		if err := gitServer.StartHTTP(); err != nil {
			ginkgo.Fail(fmt.Sprintf("Failed to start test server: %v", err))
		}
	}()
})

var _ = ginkgo.AfterSuite(func() {
	gitServer.StopHTTP()
})

// InitRepo initializes a new repository in the git server with the given
// fixture at the repoPath.
func InitRepo(s *gittestserver.GitServer, fixture, branch, repoPath string) error {
	// Create a bare repo to initialize.
	localRepo := filepath.Join(s.Root(), repoPath)
	_, err := gogit.PlainInit(localRepo, true)
	if err != nil {
		return err
	}

	// Create a new repo with the provided fixture. This creates a repo with
	// default branch as "master".
	dir, _ := os.MkdirTemp("", "github-fs-storage-*")
	dir2, _ := os.MkdirTemp("", "github-billy-fs-*")
	fsStorage := filesystem.NewStorage(osfs.New(dir), cache.NewObjectLRUDefault())
	repo, err := gogit.Init(fsStorage, osfs.New(dir2))
	if err != nil {
		return err
	}

	// Add a remote to the local repo.
	// Due to a bug in go-git, using the file protocol to push on Windows fails
	// ref: https://github.com/go-git/go-git/issues/415
	// Hence, we start a server and use the HTTP protocol to push _only_ on Windows.
	if runtime.GOOS == "windows" {
		if err = s.StartHTTP(); err != nil {
			return err
		}
		defer s.StopHTTP()
		if _, err = repo.CreateRemote(&config.RemoteConfig{
			Name: gogit.DefaultRemoteName,
			URLs: []string{s.HTTPAddressWithCredentials() + "/" + repoPath},
		}); err != nil {
			return err
		}
	} else {
		localRepoURL := getLocalURL(localRepo)
		if _, err = repo.CreateRemote(&config.RemoteConfig{
			Name: gogit.DefaultRemoteName,
			URLs: []string{localRepoURL},
		}); err != nil {
			return err
		}
	}

	if err := commitFromFixture(repo, fixture); err != nil {
		return err
	}

	// Checkout to create the target branch if it's not the default branch.
	if branch != "master" {
		if err := checkout(repo, branch); err != nil {
			return err
		}
	}

	return repo.Push(&gogit.PushOptions{
		RefSpecs: []config.RefSpec{"refs/heads/*:refs/heads/*"},
	})
}

func commitFromFixture(repo *gogit.Repository, fixture string) error {
	working, err := repo.Worktree()
	if err != nil {
		return err
	}
	fs := working.Filesystem

	if err = filepath.Walk(fixture, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return fs.MkdirAll(fs.Join(path[len(fixture):]), info.Mode())
		}

		fileBytes, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		ff, err := fs.Create(path[len(fixture):])
		if err != nil {
			return err
		}
		defer ff.Close()

		_, err = ff.Write(fileBytes)
		return err
	}); err != nil {
		return err
	}

	_, err = working.Add(".")
	if err != nil {
		return err
	}

	if _, err = working.Commit("Fixtures from "+fixture, &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  "Testbot",
			Email: "test@example.com",
			When:  time.Now(),
		},
	}); err != nil {
		return err
	}

	return nil
}

func getLocalURL(localPath string) string {
	// Three slashes after "file:", since we don't specify a host.
	// Ref: https://en.wikipedia.org/wiki/File_URI_scheme#How_many_slashes?
	return fmt.Sprintf("file:///%s", localPath)
}

func checkout(repo *gogit.Repository, branch string) error {
	branchRef := plumbing.NewBranchReferenceName(branch)
	w, err := repo.Worktree()
	if err != nil {
		return err
	}
	h, err := repo.Head()
	if err != nil {
		return err
	}
	return w.Checkout(&gogit.CheckoutOptions{
		Hash:   h.Hash(),
		Branch: branchRef,
		Create: true,
	})
}
