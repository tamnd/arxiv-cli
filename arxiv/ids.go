package arxiv

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// newStyleRe matches new-style arXiv ids: YYMM.NNNNN(vN)?
var newStyleRe = regexp.MustCompile(`^\d{4}\.\d{4,5}(v\d+)?$`)

// legacyRe matches legacy arXiv ids: archive(/sub)?/YYMMNNN(vN)?
var legacyRe = regexp.MustCompile(`^[a-z-]+(\.[A-Z]{2})?/\d{7}(v\d+)?$`)

// ParsePaperID extracts a bare arXiv id (e.g. "1706.03762") from:
//   - a bare new-style id: "1706.03762"
//   - a versioned id: "1706.03762v7"
//   - a legacy id: "hep-th/9705158"
//   - a full URL: "https://arxiv.org/abs/1706.03762v7"
//   - an export URL: "https://export.arxiv.org/abs/1706.03762"
//
// The version suffix is always stripped. Returns an error if s does not look
// like any recognized arXiv id form.
func ParsePaperID(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("paper id must not be empty")
	}

	// URL: parse and extract from path
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		u, err := url.Parse(s)
		if err != nil {
			return "", fmt.Errorf("not a valid paper id or URL: %q", s)
		}
		host := u.Host
		if host != "arxiv.org" && host != "export.arxiv.org" {
			return "", fmt.Errorf("unrecognized arxiv host %q in URL %q", host, s)
		}
		path := strings.TrimPrefix(u.Path, "/abs/")
		if path == u.Path {
			return "", fmt.Errorf("cannot extract paper id from URL %q (expected /abs/ID path)", s)
		}
		return stripVersion(path), nil
	}

	// bare new-style id (with or without version)
	if newStyleRe.MatchString(s) {
		return stripVersion(s), nil
	}

	// legacy id (with or without version)
	if legacyRe.MatchString(s) {
		return stripVersion(s), nil
	}

	return "", fmt.Errorf("not a recognized arXiv id: %q (expected YYMM.NNNNN, archive/NNNNNNN, or arxiv.org/abs/... URL)", s)
}

// stripVersion removes a trailing vN suffix from an arXiv id.
func stripVersion(id string) string {
	if idx := strings.LastIndex(id, "v"); idx >= 0 {
		suffix := id[idx+1:]
		allDigits := len(suffix) > 0
		for _, c := range suffix {
			if c < '0' || c > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			return id[:idx]
		}
	}
	return id
}
