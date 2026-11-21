package config

import (
	"fmt"

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
