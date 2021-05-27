package helpers

import (
	"fmt"

	"github.com/go-git/go-git/v5"
)

// CloningRepo pulls down the specified git repo to the destination folder
func CloningRepo(dst string, src string) error {
	/* TO-DO: This function clones the specified repo to a destination folder
	 */

	fmt.Println("Cloning Repository ...")
	// clone the given repository to the given directory
	fmt.Printf("git clone %s %s --recursive", src, dst)

	r, err := git.PlainClone(dst, false, &git.CloneOptions{
		URL:               src,
		RecurseSubmodules: git.DefaultSubmoduleRecursionDepth,
	})
	if err != nil {
		fmt.Printf("\nFailed to clone repoistory to the destination folder; %s.\n", dst)
		return err
	}

	// retrieving the branch being pointed by HEAD
	ref, err := r.Head()
	if err != nil {
		return err
	}

	// retrieving the commit object
	commit, err := r.CommitObject(ref.Hash())
	if err != nil {
		return err
	}

	fmt.Println(commit)

	return nil
}
