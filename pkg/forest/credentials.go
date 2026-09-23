package forest

import (
	"net/url"
	"strings"
)

// StripCredentials removes userinfo from a Git remote URL before output.
// It handles:
//   - Standard URL forms: https://user:token@host/path → https://host/path
//   - SCP-like forms: git@host:org/repo.git → host:org/repo.git
//   - Query strings and fragments are dropped.
//
// Malformed or unrecognized strings are returned unchanged.
func StripCredentials(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return rawURL
	}

	// SCP-like form: git@host:path or user@host:path.
	// Detected by: no scheme, contains exactly one '@' before a ':', and
	// the ':' follows the '@' without a '//' prefix.
	if !strings.Contains(rawURL, "://") {
		if at := strings.IndexByte(rawURL, '@'); at >= 0 {
			rest := rawURL[at+1:]
			// Must have a ':' for the SCP separator.
			if colon := strings.IndexByte(rest, ':'); colon >= 0 {
				host := rest[:colon]
				pathPart := rest[colon+1:]
				// Keep only host:path, dropping the user@ prefix.
				// Drop any '?' or '#' fragments in pathPart.
				if q := strings.IndexAny(pathPart, "?#"); q >= 0 {
					pathPart = pathPart[:q]
				}
				return host + ":" + pathPart
			}
		}
		return rawURL
	}

	// Standard URL: parse and strip userinfo.
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	// Drop userinfo.
	u.User = nil
	// Drop query and fragment.
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// StripRemoteURL applies StripCredentials to a single remote URL value read
// from a Git config file.
func StripRemoteURL(value string) string {
	return StripCredentials(value)
}
