package arxiv

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// errorsID is the id arXiv puts on the entry when a feed is an error rather
// than a result. It carries a fragment naming the problem when arXiv knows what
// the problem was.
const errorsID = "https://arxiv.org/api/errors"

// APIError is arXiv's own error, lifted out of the Atom feed it arrives in.
//
// The API does not answer a bad request with a status code and a plain body. It
// answers with a well-formed feed holding exactly one entry, whose title is the
// bare word "Error" and whose id is the errors URL with a fragment. All three
// forms below were captured on 2026-08-13:
//
//	400 #incorrect_id_format_for_notanid  "incorrect id format for notanid"
//	400 (no fragment)                     "Either a search_query or id_list must be specified..."
//	500 (no fragment)                     "The server encountered an internal error..."
//
// The old code looked for a title with the prefix "Error " including the
// trailing space, which never matched, so every one of these was decoded as a
// feed with one nameless paper in it.
type APIError struct {
	// Fragment is the part after the # in the entry id, empty when arXiv did
	// not name the problem.
	Fragment string
	// Message is the entry's summary, which is the human-readable version.
	Message string
}

func (e *APIError) Error() string {
	if e.Fragment != "" {
		return fmt.Sprintf("%s (%s)", e.Message, e.Fragment)
	}
	return e.Message
}

// IsUsage reports whether arXiv named the problem, which it does when the
// problem is with the request rather than with arXiv.
func (e *APIError) IsUsage() bool { return e.Fragment != "" }

// errorEntry returns the error a decoded feed carries, or nil if it carries
// results.
func errorEntry(feed *atomFeed) *APIError {
	if len(feed.Entries) != 1 {
		return nil
	}
	e := feed.Entries[0]
	id := strings.TrimSpace(e.ID)
	// Match on the id rather than the title. A paper legitimately titled
	// "Error" exists somewhere on arXiv; a paper whose id is the errors URL
	// does not.
	if !strings.HasPrefix(id, errorsID) {
		return nil
	}
	return &APIError{
		Fragment: strings.TrimPrefix(strings.TrimPrefix(id, errorsID), "#"),
		Message:  cleanText(e.Summary),
	}
}

// parseAPIError decodes a body far enough to see whether it is an error feed.
// A body that is not XML at all, which is what a too-long id list returns, is
// not an error here: it is a nil result and the caller says what it means.
func parseAPIError(body []byte) *APIError {
	var feed atomFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil
	}
	return errorEntry(&feed)
}
