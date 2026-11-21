package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

func validatePattern(p string) error {
	if p == "" {
		return fmt.Errorf("empty pattern")
	}
	if !doublestar.ValidatePattern(p) {
		return fmt.Errorf("invalid pattern %q", p)
	}
	return nil
}

func validateBranchNames(names []string) error {
	for _, n := range names {
		if n == "" || strings.HasPrefix(n, "refs/") {
			return fmt.Errorf("invalid branch name %q; use a short name such as \"main\"", n)
		}
	}
	return nil
}

// ColorNames maps ANSI color names to their numbers.
var ColorNames = map[string]string{
	"black": "0", "red": "1", "green": "2", "yellow": "3", "blue": "4", "magenta": "5", "cyan": "6", "white": "7",
	"gray": "8", "grey": "8", "bright-black": "8", "bright-red": "9", "bright-green": "10", "bright-yellow": "11",
	"bright-blue": "12", "bright-magenta": "13", "bright-cyan": "14", "bright-white": "15", "default": "",
}

// ResolveColor converts a validated color to a lipgloss color value.
func ResolveColor(v string) string {
	if n, ok := ColorNames[strings.ToLower(v)]; ok {
		return n
	}
	return v
}

func validateColor(v string) error {
	if _, ok := ColorNames[strings.ToLower(v)]; ok {
		return nil
	}
	if n, err := strconv.Atoi(v); err == nil {
		if n < 0 || n > 255 {
			return fmt.Errorf("color number %d is outside 0-255", n)
		}
		return nil
	}
	if len(v) == 7 && v[0] == '#' {
		if _, err := strconv.ParseUint(v[1:], 16, 32); err == nil {
			return nil
		}
	}
	return fmt.Errorf("invalid color %q; use a name such as \"green\", a number 0-255, or #rrggbb", v)
}
