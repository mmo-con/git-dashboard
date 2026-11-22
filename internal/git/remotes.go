package git

import (
	"bytes"
	"strings"

	"github.com/mmo-con/headquarters/internal/domain"
)

// parseRemotes parses "git config -z --get-regexp '^remote\..*\.url$'"
// output. Records are "key\nvalue" terminated by NUL.
func parseRemotes(out []byte) []domain.Remote {
	var remotes []domain.Remote
	for _, rec := range bytes.Split(out, []byte{0}) {
		if len(rec) == 0 {
			continue
		}
		key, value, _ := strings.Cut(string(rec), "\n")
		lower := strings.ToLower(key)
		if !strings.HasPrefix(lower, "remote.") || !strings.HasSuffix(lower, ".url") {
			continue
		}
		name := key[len("remote.") : len(key)-len(".url")]
		dup := false
		for i := range remotes {
			if remotes[i].Name == name {
				dup = true // multiple url values: keep the first, as git remote get-url does
			}
		}
		if !dup {
			remotes = append(remotes, domain.Remote{Name: name, URL: value})
		}
	}
	return remotes
}
