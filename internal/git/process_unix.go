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

// terminateGroup asks the complete process group of a started command to
// exit. Git removes its lock files on SIGTERM, but not on SIGKILL.
func terminateGroup(cmd *exec.Cmd) { signalGroup(cmd, syscall.SIGTERM) }

// killGroup forcibly ends the complete process group of a started command.
func killGroup(cmd *exec.Cmd) { signalGroup(cmd, syscall.SIGKILL) }

func signalGroup(cmd *exec.Cmd, sig syscall.Signal) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, sig)
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
