package data

import "regexp"

// ansiRe matches ANSI escape sequences (CSI, OSC, and simple escapes).
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x1b]*\x1b\\|\x1b[^[\]]`)

// StripANSI removes ANSI escape codes from s.
func StripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}
