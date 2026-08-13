package arxiv

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/tamnd/any-cli/kit/errs"
)

// TestMapErrExitCodes pins the table in doc 05 section 7. The exit code is the
// part of a CLI a script depends on, so it is worth asserting rather than
// assuming.
func TestMapErrExitCodes(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code int
	}{
		{"nothing wrong", nil, 0},
		{"no such paper", fmt.Errorf("2601.00001: %w", ErrNotFound), 6},
		{"timed out", context.DeadlineExceeded, 8},
		{"arxiv named the problem", &APIError{
			Fragment: "incorrect_id_format_for_notanid",
			Message:  "incorrect id format for notanid",
		}, 2},
		{"arxiv did not name it", &APIError{
			Message: "The server encountered an internal error.",
		}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := errs.ExitCode(mapErr(tc.err))
			if got != tc.code {
				t.Errorf("exit code = %d, want %d", got, tc.code)
			}
		})
	}
}

// TestMapErrKeepsArxivsWording matters because arXiv's message is more specific
// than anything this tool could write from the outside.
func TestMapErrKeepsArxivsWording(t *testing.T) {
	apiErr := &APIError{
		Fragment: "incorrect_id_format_for_notanid",
		Message:  "incorrect id format for notanid",
	}
	err := mapErr(apiErr)
	if !strings.Contains(err.Error(), apiErr.Message) {
		t.Errorf("error %q lost arxiv's message", err)
	}
	// And the original is still reachable, so a caller can look at the fragment.
	var got *APIError
	if !errors.As(err, &got) {
		t.Fatal("the wrapped error no longer unwraps to an APIError")
	}
	if got.Fragment != apiErr.Fragment {
		t.Errorf("fragment = %q, want %q", got.Fragment, apiErr.Fragment)
	}
}

// TestWindowErrorIsNotAUsageError checks the one case that is this tool's fault
// rather than the user's. Asking past the window means a bug here, and it says
// so rather than blaming the query.
func TestWindowErrorIsNotAUsageError(t *testing.T) {
	_, err := Request{Query: Term(FieldAll, "x"), Start: 9999, Max: 100}.Values()
	if err == nil {
		t.Fatal("a request past the window was allowed")
	}
	if errs.ExitCode(mapErr(err)) != 1 {
		t.Errorf("exit code = %d, want 1", errs.ExitCode(mapErr(err)))
	}
	if !strings.Contains(err.Error(), "slice the query") {
		t.Errorf("error %q does not say what to do instead", err)
	}
}
