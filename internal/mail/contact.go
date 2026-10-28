package mail

import (
	"strings"
	"time"
)

type ContactKind uint8

const (
	ContactReceived ContactKind = iota
	ContactSent
)

const (
	ContactSentWeight     = 3
	ContactReceivedWeight = 1
)

type Contact struct {
	ID            int64
	AccountID     int64
	Email         string
	Name          string
	SentCount     int64
	ReceivedCount int64
	FirstSeen     time.Time
	LastSeen      time.Time
}

type ContactUpsert struct {
	Address Address
	Kind    ContactKind
}

var systemLocalParts = []string{"mailer-daemon", "postmaster"}
var systemDomainPrefixes = []string{"bounces.", "bounce."}

// IsSystemAddress returns true for well-known bounce/daemon addresses that
// should not appear in autocomplete suggestions.
func IsSystemAddress(email string) bool {
	lower := strings.ToLower(email)
	at := strings.IndexByte(lower, '@')
	if at < 0 {
		return false
	}
	local := lower[:at]
	domain := lower[at+1:]
	for _, lp := range systemLocalParts {
		if local == lp {
			return true
		}
	}
	for _, dp := range systemDomainPrefixes {
		if strings.HasPrefix(domain, dp) {
			return true
		}
	}
	return false
}
