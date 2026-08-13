package arxiv

import (
	"fmt"
	"strings"
	"testing"
)

// errorFeed builds the feed arXiv answers a bad request with. The shape is
// copied from three real captures on 2026-08-13, down to the entry title being
// the bare word "Error" and the id being the errors URL with an optional
// fragment.
func errorFeed(fragment, message string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <link href="http://arxiv.org/api/errors" rel="self" type="application/atom+xml"/>
  <title type="html">ERROR</title>
  <id>http://arxiv.org/api/errors</id>
  <updated>2026-08-13T00:00:00-04:00</updated>
  <opensearch:totalResults xmlns:opensearch="http://a9.com/-/spec/opensearch/1.1/">1</opensearch:totalResults>
  <entry>
    <id>%s%s</id>
    <title>Error</title>
    <summary>%s</summary>
    <updated>2026-08-13T00:00:00-04:00</updated>
    <author><name>arXiv api core</name></author>
  </entry>
</feed>`, errorsID, fragment, message)
}

// The three shapes arXiv actually returned, each with the status it came with.
func TestParseAPIError(t *testing.T) {
	cases := []struct {
		name     string
		fragment string
		message  string
		usage    bool
	}{
		{
			name:     "bad id",
			fragment: "#incorrect_id_format_for_notanid",
			message:  "incorrect id format for notanid",
			usage:    true,
		},
		{
			name:    "no query at all",
			message: "Either a search_query or id_list must be specified for the classic API.",
		},
		{
			name:    "internal error",
			message: "The server encountered an internal error.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			apiErr := parseAPIError([]byte(errorFeed(tc.fragment, tc.message)))
			if apiErr == nil {
				t.Fatal("the error feed was decoded as results")
			}
			if want := strings.TrimPrefix(tc.fragment, "#"); apiErr.Fragment != want {
				t.Errorf("fragment = %q, want %q", apiErr.Fragment, want)
			}
			if apiErr.Message != tc.message {
				t.Errorf("message = %q, want %q", apiErr.Message, tc.message)
			}
			if apiErr.IsUsage() != tc.usage {
				t.Errorf("IsUsage = %v, want %v", apiErr.IsUsage(), tc.usage)
			}
			if !strings.Contains(apiErr.Error(), tc.message) {
				t.Errorf("Error() = %q, does not carry the message", apiErr.Error())
			}
		})
	}
}

// TestResultFeedIsNotAnError is the half the old code got wrong in the other
// direction. A real feed must never be read as an error.
func TestResultFeedIsNotAnError(t *testing.T) {
	if apiErr := parseAPIError([]byte(sampleFeed)); apiErr != nil {
		t.Errorf("a results feed decoded as the error %q", apiErr)
	}
	if apiErr := parseAPIError([]byte(emptyFeed)); apiErr != nil {
		t.Errorf("an empty feed decoded as the error %q", apiErr)
	}
}

// TestPaperTitledErrorIsNotAnError is why the match is on the entry id rather
// than on the title. arXiv has papers with "Error" in the title and one of them
// having exactly that title is a matter of time.
func TestPaperTitledErrorIsNotAnError(t *testing.T) {
	feed := `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <id>http://arxiv.org/abs/2401.00001v1</id>
    <title>Error</title>
    <summary>A paper about error.</summary>
  </entry>
</feed>`
	if apiErr := parseAPIError([]byte(feed)); apiErr != nil {
		t.Errorf("a paper titled Error decoded as the error %q", apiErr)
	}
}

// TestNonXMLIsNotAnError covers the HTML body arXiv sends for an over-long id
// list. It is not an Atom error, and pretending otherwise would swallow it.
func TestNonXMLIsNotAnError(t *testing.T) {
	if apiErr := parseAPIError([]byte("<html><body>Bad Request</body></html>")); apiErr != nil {
		t.Errorf("an HTML body decoded as the error %q", apiErr)
	}
	if apiErr := parseAPIError([]byte("Rate exceeded.")); apiErr != nil {
		t.Errorf("a plain text body decoded as the error %q", apiErr)
	}
}
