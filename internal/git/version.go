package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// MinimumVersion is the oldest supported Git release.
var MinimumVersion = Version{Major: 2, Minor: 34}

// stashHeaderVersion is the first release whose "status --porcelain=v2
// --show-stash" reports the number of stash entries.
var stashHeaderVersion = Version{Major: 2, Minor: 35}

type Version struct {
	Major, Minor, Patch int
	Raw                 string
}

func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch) }

// AtLeast reports whether v is the same as or newer than o.
func (v Version) AtLeast(o Version) bool {
	if v.Major != o.Major {
		return v.Major > o.Major
	}
	if v.Minor != o.Minor {
		return v.Minor > o.Minor
	}
	return v.Patch >= o.Patch
}

// ParseVersion parses the output of "git version", for example
// "git version 2.39.2 (Apple Git-143)".
func ParseVersion(out string) (Version, error) {
	fields := strings.Fields(out)
	if len(fields) < 3 || fields[0] != "git" || fields[1] != "version" {
		return Version{}, fmt.Errorf("unexpected git version output %q", strings.TrimSpace(out))
	}
	parts := strings.Split(fields[2], ".")
	nums := [3]int{}
	for i := 0; i < len(parts) && i < 3; i++ {
		digits := parts[i]
		if j := strings.IndexFunc(digits, func(r rune) bool { return r < '0' || r > '9' }); j >= 0 {
			digits = digits[:j]
		}
		n, err := strconv.Atoi(digits)
		if err != nil {
			if i < 2 {
				return Version{}, fmt.Errorf("unexpected git version %q", fields[2])
			}
			break
		}
		nums[i] = n
	}
	return Version{Major: nums[0], Minor: nums[1], Patch: nums[2], Raw: fields[2]}, nil
}

// Version runs "git version".
func (c *Client) Version(ctx context.Context) (Version, error) {
	res, err := c.run.run(ctx, "", true, "version")
	if err != nil {
		return Version{}, err
	}
	return ParseVersion(string(res.stdout))
}

// versionTimeout bounds "git version" when a client is opened.
const versionTimeout = 10 * time.Second

// Open returns a client for executable whose version has been checked, so
// that it uses every feature of the installed Git. Clients for production use
// are created with Open; New alone yields a client that assumes the minimum
// version.
func Open(ctx context.Context, executable string) (*Client, Version, error) {
	c := New(executable)
	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	v, err := c.CheckVersion(ctx)
	if err != nil {
		return nil, v, err
	}
	return c, v, nil
}

// CheckVersion fails if the installed Git is older than MinimumVersion and
// enables the features of newer releases otherwise.
func (c *Client) CheckVersion(ctx context.Context) (Version, error) {
	v, err := c.Version(ctx)
	if err != nil {
		return v, fmt.Errorf("cannot run %s: %w", c.run.executable, err)
	}
	if !v.AtLeast(MinimumVersion) {
		return v, fmt.Errorf("git %s is too old; headquarters requires git %d.%d or newer", v.Raw, MinimumVersion.Major, MinimumVersion.Minor)
	}
	c.stashHeader = v.AtLeast(stashHeaderVersion)
	return v, nil
}
