//go:build unix

package git

import (
	"os/exec"
	"syscall"
)

// isolate starts the process in a new session without a controlling terminal.
// ssh and credential helpers therefore cannot open /dev/tty, and the process
// becomes the leader of its own process group.
func isolate(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// killGroup terminates the complete process group of a started command.
func killGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

// ownedByOtherUser reports whether path belongs to a different user than the
// current process.
func ownedByOtherUser(path string) bool {
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		return false
	}
	return int(st.Uid) != syscall.Getuid()
}
