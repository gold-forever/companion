package instance

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// sleeper starts a program named as the companion is, which runs until stopped.
func sleeper(t *testing.T) *exec.Cmd {
	t.Helper()
	data, err := os.ReadFile("/bin/sleep")
	if err != nil {
		t.Skip("no /bin/sleep to stand in for the companion")
	}
	exe := filepath.Join(t.TempDir(), "gold-forever-companion")
	if err := os.WriteFile(exe, data, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	return cmd
}

func TestACopysProcessRunsUntilItHasExited(t *testing.T) {
	cmd := sleeper(t)
	info := Info{PID: cmd.Process.Pid}
	if !Running(info) {
		t.Fatal("a running companion process was not seen")
	}
	cmd.Process.Kill()
	cmd.Wait()
	if Running(info) {
		t.Error("an exited process was taken for running")
	}
}

// After a crash the PID may be another program's: only a process named as the companion counts.
func TestAProcessNotNamedAsTheCompanionIsNotACopy(t *testing.T) {
	if Running(Info{PID: os.Getpid()}) {
		t.Error("the test binary was taken for a companion")
	}
	if Running(Info{}) {
		t.Error("a file without a PID names no process")
	}
}

func TestClosingACopyStopsItsProcess(t *testing.T) {
	cmd := sleeper(t)
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	if err := Close(Info{PID: cmd.Process.Pid}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("the copy still runs after Close")
	}
}

func TestClosingRefusesAProcessThatIsNotACopy(t *testing.T) {
	if err := Close(Info{PID: os.Getpid()}); err == nil {
		t.Error("Close would stop a process not named as the companion")
	}
}
