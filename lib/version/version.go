package version

import "regexp"

// Release archives and D2.js set Version at link time.
var Version = "(devel)"

func OnlyNumbers() string {
	re, err := regexp.Compile("[0-9]+.[0-9]+.[0-9]+")
	if err != nil {
		return ""
	}
	return re.FindString(Version)
}
