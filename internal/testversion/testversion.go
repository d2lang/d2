// Package testversion pins version metadata in the existing diagram goldens.
package testversion

import "github.com/d2lang/d2/lib/version"

func init() { version.Version = "v0.8.1-HEAD" }
