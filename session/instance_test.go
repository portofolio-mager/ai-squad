package session

import (
	"testing"

	"ai-squad/session/git"

	"github.com/stretchr/testify/assert"
)

func TestNewInstance(t *testing.T) {
	t.Run("creates instance with HEAD base ref", func(t *testing.T) {
		instance, err := NewInstance(InstanceOptions{
			Title:   "test-instance",
			Path:    ".",
			Program: "claude",
			BaseRef: "HEAD",
		})

		assert.NoError(t, err)
		assert.Equal(t, "test-instance", instance.Title)
		assert.Equal(t, "HEAD", instance.BaseRef)
		assert.Equal(t, "claude", instance.Program)
	})

	t.Run("creates instance with main base ref", func(t *testing.T) {
		instance, err := NewInstance(InstanceOptions{
			Title:   "test-instance-main",
			Path:    ".",
			Program: "claude",
			BaseRef: "main",
		})

		assert.NoError(t, err)
		assert.Equal(t, "test-instance-main", instance.Title)
		assert.Equal(t, "main", instance.BaseRef)
		assert.Equal(t, "claude", instance.Program)
	})

	t.Run("creates instance with empty base ref", func(t *testing.T) {
		instance, err := NewInstance(InstanceOptions{
			Title:   "test-instance-empty",
			Path:    ".",
			Program: "claude",
			BaseRef: "",
		})

		assert.NoError(t, err)
		assert.Equal(t, "test-instance-empty", instance.Title)
		assert.Equal(t, "", instance.BaseRef)
		assert.Equal(t, "claude", instance.Program)
	})
}

func TestInstanceDataSerialization(t *testing.T) {
	t.Run("serializes BaseRef field", func(t *testing.T) {
		instance, err := NewInstance(InstanceOptions{
			Title:   "test-serialize",
			Path:    ".",
			Program: "claude",
			BaseRef: "main",
		})
		assert.NoError(t, err)

		data := instance.ToInstanceData()
		assert.Equal(t, "main", data.BaseRef)
		assert.Equal(t, "test-serialize", data.Title)
	})

	t.Run("deserializes BaseRef field", func(t *testing.T) {
		data := InstanceData{
			Title:   "test-deserialize",
			Path:    ".",
			Branch:  "test-branch",
			Status:  Ready,
			BaseRef: "HEAD",
			Program: "claude",
			Worktree: GitWorktreeData{
				RepoPath:     "/tmp/repo",
				WorktreePath: "/tmp/worktree",
				SessionName:  "test-deserialize",
				BranchName:   "test-branch",
			},
		}

		instance, err := FromInstanceData(data)
		assert.NoError(t, err)
		assert.Equal(t, "HEAD", instance.BaseRef)
		assert.Equal(t, "test-deserialize", instance.Title)
	})

}

func TestGetSessionPath(t *testing.T) {
	const repoPath = "/home/user/project"
	const worktreePath = "/tmp/worktree/test-session_1234567890"

	testWorktree := git.NewGitWorktreeFromStorage(
		repoPath,
		worktreePath,
		"test-session",
		"test-branch",
		"abc123",
	)

	tests := []struct {
		name     string
		instance *Instance
		want     string
		wantErr  bool
		errMsg   string
	}{
		{
			name: "at repository root",
			instance: &Instance{
				Path:        repoPath,
				gitWorktree: testWorktree,
			},
			want:    worktreePath,
			wantErr: false,
		},
		{
			name: "in subdirectory",
			instance: &Instance{
				Path:        "/home/user/project/src/api",
				gitWorktree: testWorktree,
			},
			want:    "/tmp/worktree/test-session_1234567890/src/api",
			wantErr: false,
		},
		{
			name: "started instance (resume case)",
			instance: &Instance{
				Path:        "/home/user/project/src",
				started:     true,
				gitWorktree: testWorktree,
			},
			want:    "/tmp/worktree/test-session_1234567890/src",
			wantErr: false,
		},
		{
			name: "nil worktree",
			instance: &Instance{
				Path:        "/home/user/project/src",
				gitWorktree: nil,
			},
			want:    "",
			wantErr: true,
			errMsg:  "git worktree not initialized",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.instance.GetSessionPath()

			if tt.wantErr {
				assert.Error(t, err)
				assert.EqualError(t, err, tt.errMsg)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
