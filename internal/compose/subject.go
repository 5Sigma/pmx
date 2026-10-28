package compose

import (
	"regexp"
	"strings"
)

var (
	reReplyPrefix = regexp.MustCompile(`(?i)^\s*(re|aw|sv|r)\s*:\s*`)
	reFwdPrefix   = regexp.MustCompile(`(?i)^\s*(fwd|fw)\s*:\s*`)
)

// StripReplyPrefixes removes leading Re:/RE:/AW: etc. chains.
// Loops until none remain so "Re: Re: Hello" → "Hello".
func StripReplyPrefixes(s string) string {
	for reReplyPrefix.MatchString(s) {
		s = reReplyPrefix.ReplaceAllString(s, "")
	}
	return strings.TrimSpace(s)
}

// StripAllPrefixes strips both reply and forward prefixes.
func StripAllPrefixes(s string) string {
	changed := true
	for changed {
		prev := s
		s = reReplyPrefix.ReplaceAllString(s, "")
		s = reFwdPrefix.ReplaceAllString(s, "")
		s = strings.TrimSpace(s)
		changed = s != prev
	}
	return s
}
