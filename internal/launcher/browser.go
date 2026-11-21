package launcher

import (
	"errors"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
)

// BrowserURL converts a remote URL to an HTTPS URL. HTTPS remotes are used as
// they are, without credentials and without a ".git" suffix. SSH remotes use
// the same host and repository path, without the SSH user or port.
func BrowserURL(remote string) (string, bool) {
	remote = strings.TrimSpace(remote)
	var host, path string
	switch {
	case strings.HasPrefix(remote, "https://") || strings.HasPrefix(remote, "http://"):
		u, err := url.Parse(remote)
		if err != nil || u.Host == "" {
			return "", false
		}
		u.User = nil
		u.Scheme = "https"
		u.Path = strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), ".git")
		return u.String(), true
	case strings.HasPrefix(remote, "ssh://"):
		u, err := url.Parse(remote)
		if err != nil {
			return "", false
		}
		host, path = u.Hostname(), u.Path
	case strings.Contains(remote, ":") && !strings.Contains(remote, "://"):
		// scp-like syntax: [user@]host:path
		hostPart, p, _ := strings.Cut(remote, ":")
		if i := strings.LastIndexByte(hostPart, '@'); i >= 0 {
			hostPart = hostPart[i+1:]
		}
		host, path = hostPart, p
	default:
		return "", false
	}
	host = strings.ToLower(host)
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	u, err := url.Parse("https://" + host)
	if err != nil || u.Host != host || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	if path == "" {
		return "", false
	}
	u.Path = "/" + path
	return u.String(), true
}

// OpenBrowser opens url with xdg-open on Linux or open on macOS. The opener
// runs detached from the terminal.
func OpenBrowser(target string) error {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	path, err := Resolve(opener)
	if err != nil {
		return errors.New("cannot open browser: " + err.Error())
	}
	cmd := exec.Command(path, target)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
