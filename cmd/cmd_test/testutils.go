package cmd_test

import (
	"os/exec"
)

type MockCmdExec struct {
	RunFunc      func(cmd *exec.Cmd) error
	OutputFunc   func(cmd *exec.Cmd) ([]byte, error)
	LookPathFunc func(file string) (string, error)
}

func (e MockCmdExec) Run(cmd *exec.Cmd) error {
	return e.RunFunc(cmd)
}

func (e MockCmdExec) Output(cmd *exec.Cmd) ([]byte, error) {
	return e.OutputFunc(cmd)
}

func (e MockCmdExec) LookPath(file string) (string, error) {
	if e.LookPathFunc != nil {
		return e.LookPathFunc(file)
	}
	// Default: return success to not break existing tests
	return "/usr/bin/" + file, nil
}
