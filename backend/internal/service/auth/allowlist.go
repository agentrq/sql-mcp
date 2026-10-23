package auth

import (
	"fmt"
	"regexp"
	"strings"
)

// Allowlist decides which emails may sign in. Each entry is an exact address,
// a pattern where `*` matches any run of characters (`*@agentrq.com`), or a
// regular expression written as `re:<expr>`. Matching ignores case and always
// covers the whole address, so `*@agentrq.com` does not admit
// `x@agentrq.com.evil.com`.
//
// An empty list admits nobody: an unset variable must not open the app.
type Allowlist struct {
	entries  []string
	patterns []*regexp.Regexp
}

// NewAllowlist compiles entries, refusing one that is not a valid pattern.
func NewAllowlist(entries []string) (*Allowlist, error) {
	a := &Allowlist{}
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		var expr string
		if raw, ok := strings.CutPrefix(entry, "re:"); ok {
			expr = "(?i)^(?:" + raw + ")$"
		} else {
			parts := strings.Split(entry, "*")
			for i, p := range parts {
				parts[i] = regexp.QuoteMeta(p)
			}
			expr = "(?i)^" + strings.Join(parts, ".*") + "$"
		}
		re, err := regexp.Compile(expr)
		if err != nil {
			return nil, fmt.Errorf("ALLOWED_EMAILS: %q is not a valid pattern: %w", entry, err)
		}
		a.entries = append(a.entries, entry)
		a.patterns = append(a.patterns, re)
	}
	return a, nil
}

// Entries are the list as written, blanks dropped.
func (a *Allowlist) Entries() []string { return append([]string(nil), a.entries...) }

// Match returns the first entry admitting email, "" when none does.
func (a *Allowlist) Match(email string) string {
	email = strings.TrimSpace(email)
	if email == "" || strings.ContainsAny(email, " \t\r\n") {
		return ""
	}
	for i, re := range a.patterns {
		if re.MatchString(email) {
			return a.entries[i]
		}
	}
	return ""
}

// Allows reports whether email may sign in.
func (a *Allowlist) Allows(email string) bool { return a.Match(email) != "" }

// Empty reports whether nobody can sign in.
func (a *Allowlist) Empty() bool { return len(a.patterns) == 0 }
