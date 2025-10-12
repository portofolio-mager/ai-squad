package git

import (
	"ai-squad/log"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// Setup creates a new worktree for the session
func (g *GitWorktree) Setup() error {
	// Ensure worktrees directory exists early
	worktreesDir, err := getWorktreeDirectory()
	if err != nil {
		return fmt.Errorf("failed to get worktree directory: %w", err)
	}

	// Create directory
	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		return fmt.Errorf("failed to create worktrees directory: %w", err)
	}

	// Open repository for branch checks
	repo, err := git.PlainOpen(g.repoPath)
	if err != nil {
		return fmt.Errorf("failed to open repository: %w", err)
	}

	// Check for local branch first - this handles branches with "/" in their names
	branchRef := plumbing.NewBranchReferenceName(g.branchName)
	if _, err := repo.Reference(branchRef, false); err == nil {
		// Local branch exists, use setupFromExistingBranch
		return g.setupFromExistingBranch()
	}

	// Also check using git command for local branch (more reliable for complex branch names)
	if _, err := g.runGitCommand(g.repoPath, "rev-parse", "--verify", "refs/heads/"+g.branchName); err == nil {
		// Local branch exists, use setupFromExistingBranch
		return g.setupFromExistingBranch()
	}

	// Check for remote branch
	remoteBranchName := g.branchName
	// Only add origin/ prefix if it's not already a remote reference
	if !strings.HasPrefix(g.branchName, "origin/") && !strings.Contains(g.branchName, "/") {
		remoteBranchName = "origin/" + g.branchName
	}

	// Check git command line to see if remote branch exists
	_, err = g.runGitCommand(g.repoPath, "rev-parse", "--verify", "refs/remotes/"+remoteBranchName)
	if err == nil {
		// Remote branch exists, use setupFromExistingBranch
		g.branchName = remoteBranchName // Update to use full remote name
		return g.setupFromExistingBranch()
	}

	// Also check without origin prefix in case it's a different remote
	refs, _ := repo.References()
	if refs != nil {
		err := refs.ForEach(func(ref *plumbing.Reference) error {
			refName := ref.Name().String()
			// Check if this is a remote branch that matches our branch name
			if strings.HasPrefix(refName, "refs/remotes/") && strings.HasSuffix(refName, "/"+g.branchName) {
				g.branchName = strings.TrimPrefix(refName, "refs/remotes/")
				return fmt.Errorf("found") // Use error to break out of ForEach
			}
			return nil
		})
		if err != nil && err.Error() == "found" {
			return g.setupFromExistingBranch()
		}
	}

	// Branch doesn't exist anywhere, create new worktree from HEAD
	return g.setupNewWorktree()
}

// setupFromExistingBranch creates a worktree from an existing branch
func (g *GitWorktree) setupFromExistingBranch() error {
	// Directory already created in Setup(), skip duplicate creation

	// Clean up any existing worktree first
	_, _ = g.runGitCommand(g.repoPath, "worktree", "remove", "-f", g.worktreePath) // Ignore error if worktree doesn't exist

	// Prune any stale worktree references
	_, _ = g.runGitCommand(g.repoPath, "worktree", "prune")

	// Check if this is a remote branch that needs to be created locally
	isRemoteBranch := false
	remoteBranchName := ""

	// Check if this is actually a remote branch (starts with "origin/" or other remote prefix)
	if strings.HasPrefix(g.branchName, "origin/") {
		parts := strings.SplitN(g.branchName, "/", 2)
		if len(parts) == 2 {
			remoteBranchName = g.branchName
			g.branchName = parts[1] // Use just the branch name without remote
			isRemoteBranch = true
		}
	} else {
		// Check if it's a different remote (not origin)
		refs, err := g.runGitCommand(g.repoPath, "remote")
		if err == nil && len(refs) > 0 {
			remotes := strings.Split(strings.TrimSpace(string(refs)), "\n")
			for _, remote := range remotes {
				if remote != "" && strings.HasPrefix(g.branchName, remote+"/") {
					parts := strings.SplitN(g.branchName, "/", 2)
					if len(parts) == 2 {
						remoteBranchName = g.branchName
						g.branchName = parts[1]
						isRemoteBranch = true
						break
					}
				}
			}
		}
	}

	// First, check if the branch is already checked out in another worktree
	output, err := g.runGitCommand(g.repoPath, "worktree", "list", "--porcelain")
	if err == nil {
		// Check if branch is already checked out
		lines := strings.Split(string(output), "\n")
		branchRef := fmt.Sprintf("branch refs/heads/%s", g.branchName)
		for _, line := range lines {
			if strings.TrimSpace(line) == branchRef {
				// Branch is already checked out elsewhere
				// Create a new branch name with a suffix for this worktree
				timestamp := time.Now().Format("20060102-150405")
				newBranchName := fmt.Sprintf("%s-worktree-%s", g.branchName, timestamp)

				// For remote branches, fetch latest
				if isRemoteBranch {
					if _, err := g.runGitCommand(g.repoPath, "fetch", "origin", g.branchName); err != nil {
						log.WarningLog.Printf("failed to fetch latest changes for branch %s: %v", g.branchName, err)
					}

					// Create new branch from remote
					if _, err := g.runGitCommand(g.repoPath, "worktree", "add", "-b", newBranchName, g.worktreePath, remoteBranchName); err != nil {
						return fmt.Errorf("failed to create worktree with new branch %s from %s: %w", newBranchName, remoteBranchName, err)
					}

					// Set up tracking to the remote branch
					if _, err := g.runGitCommand(g.worktreePath, "branch", "--set-upstream-to="+remoteBranchName, newBranchName); err != nil {
						log.WarningLog.Printf("failed to set upstream tracking for branch %s: %v", newBranchName, err)
					}
				} else {
					// For local branches, create new branch from the existing one
					if _, err := g.runGitCommand(g.repoPath, "worktree", "add", "-b", newBranchName, g.worktreePath, g.branchName); err != nil {
						return fmt.Errorf("failed to create worktree with new branch %s from %s: %w", newBranchName, g.branchName, err)
					}
				}

				// Update branch name to the new one
				g.branchName = newBranchName
				log.InfoLog.Printf("Branch was already checked out, created new branch: %s", newBranchName)

				return nil
			}
		}
	}

	// Branch is not checked out elsewhere
	if isRemoteBranch {
		// Fetch the latest changes for this remote branch
		if _, err := g.runGitCommand(g.repoPath, "fetch", "origin", g.branchName+":"+g.branchName); err != nil {
			// If fetch fails, try without updating the local ref
			if _, err := g.runGitCommand(g.repoPath, "fetch", "origin", g.branchName); err != nil {
				log.WarningLog.Printf("failed to fetch latest changes for branch %s: %v", g.branchName, err)
			}
		}

		// Check if local branch already exists
		localBranchExists := false
		if _, err := g.runGitCommand(g.repoPath, "rev-parse", "--verify", "refs/heads/"+g.branchName); err == nil {
			localBranchExists = true
		}

		if localBranchExists {
			// Local branch exists, update it to match remote and checkout
			// First, create worktree with the existing local branch
			if _, err := g.runGitCommand(g.repoPath, "worktree", "add", g.worktreePath, g.branchName); err != nil {
				return fmt.Errorf("failed to create worktree from branch %s: %w", g.branchName, err)
			}

			// Then reset it to the remote branch to get latest changes
			if _, err := g.runGitCommand(g.worktreePath, "reset", "--hard", remoteBranchName); err != nil {
				return fmt.Errorf("failed to reset branch %s to %s: %w", g.branchName, remoteBranchName, err)
			}
		} else {
			// Create new local branch tracking the remote
			if _, err := g.runGitCommand(g.repoPath, "worktree", "add", "-b", g.branchName, g.worktreePath, remoteBranchName); err != nil {
				return fmt.Errorf("failed to create worktree with new branch %s tracking %s: %w", g.branchName, remoteBranchName, err)
			}
		}

		// Set up tracking information
		if _, err := g.runGitCommand(g.worktreePath, "branch", "--set-upstream-to="+remoteBranchName, g.branchName); err != nil {
			log.WarningLog.Printf("failed to set upstream tracking for branch %s: %v", g.branchName, err)
		}
	} else {
		// For local branches, just add the worktree
		if _, err := g.runGitCommand(g.repoPath, "worktree", "add", g.worktreePath, g.branchName); err != nil {
			return fmt.Errorf("failed to create worktree from branch %s: %w", g.branchName, err)
		}
	}

	// Get and store the commit hash
	commitOutput, err := g.runGitCommand(g.repoPath, "rev-parse", g.branchName)
	if err == nil {
		g.baseCommitSHA = strings.TrimSpace(string(commitOutput))
	}

	return nil
}

// setupNewWorktree creates a new worktree from HEAD
func (g *GitWorktree) setupNewWorktree() error {
	// Ensure worktrees directory exists
	worktreesDir := filepath.Join(g.repoPath, "worktrees")
	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		return fmt.Errorf("failed to create worktrees directory: %w", err)
	}

	// Clean up any existing worktree first
	_, _ = g.runGitCommand(g.repoPath, "worktree", "remove", "-f", g.worktreePath) // Ignore error if worktree doesn't exist

	// Prune any stale worktree references
	_, _ = g.runGitCommand(g.repoPath, "worktree", "prune")

	// Open the repository
	repo, err := git.PlainOpen(g.repoPath)
	if err != nil {
		return fmt.Errorf("failed to open repository: %w", err)
	}

	// Clean up any existing branch or reference
	if err := g.cleanupExistingBranch(repo); err != nil {
		// If we can't clean up the branch, it might be checked out elsewhere
		// Try to list worktrees to provide more context
		worktreeListOutput, _ := g.runGitCommand(g.repoPath, "worktree", "list")
		return fmt.Errorf("failed to cleanup existing branch '%s': %w\nCurrent worktrees:\n%s", g.branchName, err, worktreeListOutput)
	}

	// First, fetch the latest from origin to ensure we have the most recent remote state
	if _, err := g.runGitCommand(g.repoPath, "fetch", "origin"); err != nil {
		// If fetch fails, log it but continue - we might be offline
		fmt.Printf("Warning: Could not fetch from origin: %v\n", err)
	}

	// Get the remote HEAD reference to determine the default branch
	remoteHeadOutput, err := g.runGitCommand(g.repoPath, "symbolic-ref", "refs/remotes/origin/HEAD")
	var targetCommit string
	if err != nil {
		// If we can't get the remote HEAD, fall back to trying origin/main or origin/master
		mainOutput, mainErr := g.runGitCommand(g.repoPath, "rev-parse", "origin/main")
		masterOutput, masterErr := g.runGitCommand(g.repoPath, "rev-parse", "origin/master")

		if mainErr == nil {
			targetCommit = strings.TrimSpace(string(mainOutput))
		} else if masterErr == nil {
			targetCommit = strings.TrimSpace(string(masterOutput))
		} else {
			// Final fallback to local HEAD if we can't find remote default branch
			output, err := g.runGitCommand(g.repoPath, "rev-parse", "HEAD")
			if err != nil {
				if strings.Contains(err.Error(), "fatal: ambiguous argument 'HEAD'") ||
					strings.Contains(err.Error(), "fatal: not a valid object name") ||
					strings.Contains(err.Error(), "fatal: HEAD: not a valid object name") {
					return fmt.Errorf("this appears to be a brand new repository: please create an initial commit before creating an instance")
				}
				return fmt.Errorf("failed to get HEAD commit hash: %w", err)
			}
			targetCommit = strings.TrimSpace(string(output))
			fmt.Println("Warning: Could not determine remote default branch, using local HEAD")
		}
	} else {
		// Successfully got remote HEAD, extract the branch name and get its commit
		remoteHead := strings.TrimSpace(string(remoteHeadOutput))
		// remoteHead will be something like "refs/remotes/origin/main"
		// We need to get the commit it points to
		commitOutput, err := g.runGitCommand(g.repoPath, "rev-parse", remoteHead)
		if err != nil {
			return fmt.Errorf("failed to get commit for remote HEAD %s: %w", remoteHead, err)
		}
		targetCommit = strings.TrimSpace(string(commitOutput))
	}

	g.baseCommitSHA = targetCommit

	// Create a new worktree from the target commit (remote HEAD or fallback)
	// This ensures we start from the latest state of the main branch
	if _, err := g.runGitCommand(g.repoPath, "worktree", "add", "-b", g.branchName, g.worktreePath, targetCommit); err != nil {
		// Check if the branch already exists
		if strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "is not a valid branch name") {
			// Try to get more information about existing branches
			branchListOutput, _ := g.runGitCommand(g.repoPath, "branch", "-a")
			return fmt.Errorf("failed to create worktree with branch '%s' from commit %s: %w\nExisting branches:\n%s", g.branchName, targetCommit, err, branchListOutput)
		}
		return fmt.Errorf("failed to create worktree from commit %s with branch '%s': %w\nWorktree path: %s", targetCommit, g.branchName, err, g.worktreePath)
	}

	return nil
}

// Cleanup removes the worktree and associated branch
func (g *GitWorktree) Cleanup() error {
	var errs []error

	// Check if worktree path exists before attempting removal
	if _, err := os.Stat(g.worktreePath); err == nil {
		// Remove the worktree using git command
		if _, err := g.runGitCommand(g.repoPath, "worktree", "remove", "-f", g.worktreePath); err != nil {
			errs = append(errs, err)
		}
	} else if !os.IsNotExist(err) {
		// Only append error if it's not a "not exists" error
		errs = append(errs, fmt.Errorf("failed to check worktree path: %w", err))
	}

	// Open the repository for branch cleanup
	repo, err := git.PlainOpen(g.repoPath)
	if err != nil {
		// If repo doesn't exist, we can't clean up the branch but that's okay
		if os.IsNotExist(err) || strings.Contains(err.Error(), "repository does not exist") {
			log.InfoLog.Printf("Repository doesn't exist at %s, skipping branch cleanup", g.repoPath)
			// Still try to prune worktrees
			if err := g.Prune(); err != nil {
				errs = append(errs, err)
			}
			return g.combineErrors(errs)
		}
		errs = append(errs, fmt.Errorf("failed to open repository for cleanup: %w", err))
		return g.combineErrors(errs)
	}

	branchRef := plumbing.NewBranchReferenceName(g.branchName)

	// Check if branch exists before attempting removal
	if _, err := repo.Reference(branchRef, false); err == nil {
		// Check if branch is checked out in main repo
		isCheckedOut, _ := g.IsBranchCheckedOut()
		if !isCheckedOut {
			// First try normal deletion
			if err := repo.Storer.RemoveReference(branchRef); err != nil {
				// If that fails, try command line force delete
				if _, cmdErr := g.runGitCommand(g.repoPath, "branch", "-D", g.branchName); cmdErr != nil {
					errs = append(errs, fmt.Errorf("failed to remove branch %s: %w (force delete also failed: %v)", g.branchName, err, cmdErr))
				}
			}
		} else {
			// Branch is checked out in main repo, skip deletion but log it
			log.WarningLog.Printf("branch %s is checked out in main repository, skipping branch deletion", g.branchName)
		}
	} else if err != plumbing.ErrReferenceNotFound {
		errs = append(errs, fmt.Errorf("error checking branch %s existence: %w", g.branchName, err))
	}

	// Prune the worktree to clean up any remaining references
	if err := g.Prune(); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return g.combineErrors(errs)
	}

	return nil
}

// ForceCleanup performs aggressive cleanup of the worktree and branch
// This method attempts multiple fallback strategies to ensure cleanup succeeds
func (g *GitWorktree) ForceCleanup() error {
	var errs []error

	// First try normal cleanup
	if err := g.Cleanup(); err == nil {
		return nil
	} else {
		errs = append(errs, fmt.Errorf("normal cleanup failed: %w", err))
	}

	// Check if branch is checked out in main repo
	isCheckedOut, _ := g.IsBranchCheckedOut()
	if isCheckedOut {
		// Log that we'll skip branch deletion
		log.WarningLog.Printf("branch %s is checked out in main repository, will skip branch deletion", g.branchName)
	}

	// Try more aggressive cleanup methods

	// 1. Force remove worktree with git command (ignoring current state)
	if _, err := g.runGitCommand(g.repoPath, "worktree", "remove", "-f", g.worktreePath); err != nil {
		// If that fails, try to manually remove from worktree list
		if _, err2 := g.runGitCommand(g.repoPath, "worktree", "prune"); err2 != nil {
			errs = append(errs, fmt.Errorf("failed to prune after force remove: %w", err2))
		}
	}

	// 2. Force delete the branch with -D flag
	if g.branchName != "" && !isCheckedOut {
		// Try force delete with -D
		if _, err := g.runGitCommand(g.repoPath, "branch", "-D", g.branchName); err != nil {
			errs = append(errs, fmt.Errorf("failed to force delete branch %s: %w", g.branchName, err))

			// If branch delete fails, try to remove the ref directly
			repo, openErr := git.PlainOpen(g.repoPath)
			if openErr == nil {
				branchRef := plumbing.NewBranchReferenceName(g.branchName)
				if err := repo.Storer.RemoveReference(branchRef); err != nil {
					errs = append(errs, fmt.Errorf("failed to remove branch ref directly: %w", err))
				}
			} else if !os.IsNotExist(openErr) && !strings.Contains(openErr.Error(), "repository does not exist") {
				// Only log error if it's not just a missing repo
				errs = append(errs, fmt.Errorf("failed to open repo for ref cleanup: %w", openErr))
			}
		}
	}

	// 3. Manual filesystem cleanup as last resort
	if g.worktreePath != "" && g.worktreePath != "/" && strings.Contains(g.worktreePath, "worktrees") {
		if err := os.RemoveAll(g.worktreePath); err != nil {
			if !os.IsNotExist(err) {
				errs = append(errs, fmt.Errorf("failed to manually remove worktree directory: %w", err))
			}
		}
	}

	// 4. Final prune to clean up any remaining references
	if _, err := g.runGitCommand(g.repoPath, "worktree", "prune"); err != nil {
		errs = append(errs, fmt.Errorf("final prune failed: %w", err))
	}

	if len(errs) > 0 {
		return g.combineErrors(errs)
	}

	return nil
}

// Remove removes the worktree but keeps the branch
func (g *GitWorktree) Remove() error {
	// Remove the worktree using git command
	if _, err := g.runGitCommand(g.repoPath, "worktree", "remove", "-f", g.worktreePath); err != nil {
		return fmt.Errorf("failed to remove worktree: %w", err)
	}

	return nil
}

// Prune removes all working tree administrative files and directories
func (g *GitWorktree) Prune() error {
	if _, err := g.runGitCommand(g.repoPath, "worktree", "prune"); err != nil {
		return fmt.Errorf("failed to prune worktrees: %w", err)
	}
	return nil
}

// CleanupWorktrees removes all worktrees and their associated branches
func CleanupWorktrees() error {
	worktreesDir, err := getWorktreeDirectory()
	if err != nil {
		return fmt.Errorf("failed to get worktree directory: %w", err)
	}

	entries, err := os.ReadDir(worktreesDir)
	if err != nil {
		return fmt.Errorf("failed to read worktree directory: %w", err)
	}

	// Get a list of all branches associated with worktrees
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to list worktrees: %w", err)
	}

	// Parse the output to extract branch names
	worktreeBranches := make(map[string]string)
	currentWorktree := ""
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "worktree ") {
			currentWorktree = strings.TrimPrefix(line, "worktree ")
		} else if strings.HasPrefix(line, "branch ") {
			branchPath := strings.TrimPrefix(line, "branch ")
			// Extract branch name from refs/heads/branch-name
			branchName := strings.TrimPrefix(branchPath, "refs/heads/")
			if currentWorktree != "" {
				worktreeBranches[currentWorktree] = branchName
			}
		}
	}

	for _, entry := range entries {
		if entry.IsDir() {
			worktreePath := filepath.Join(worktreesDir, entry.Name())

			// Delete the branch associated with this worktree if found
			for path, branch := range worktreeBranches {
				if strings.Contains(path, entry.Name()) {
					// Delete the branch
					deleteCmd := exec.Command("git", "branch", "-D", branch)
					if err := deleteCmd.Run(); err != nil {
						// Log the error but continue with other worktrees
						log.ErrorLog.Printf("failed to delete branch %s: %v", branch, err)
					}
					break
				}
			}

			// Remove the worktree directory
			os.RemoveAll(worktreePath)
		}
	}

	// You have to prune the cleaned up worktrees.
	cmd = exec.Command("git", "worktree", "prune")
	_, err = cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to prune worktrees: %w", err)
	}

	return nil
}
