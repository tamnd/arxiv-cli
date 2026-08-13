package arxiv

import (
	"context"
	"errors"
	"net"

	"github.com/tamnd/any-cli/kit/errs"
)

// mapErr classifies a library error into the shared kit taxonomy, so every
// surface reports the same exit code and the same HTTP status for the same
// failure. Doc 05 section 7 of spec 3006 has the table.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	// arXiv's own rejection, lifted out of the error feed. When arXiv named the
	// problem it is the query that is wrong, so it exits 2 carrying arXiv's
	// wording rather than a paraphrase of it.
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		if apiErr.IsUsage() {
			return errs.Wrap(errs.KindUsage, err, "arxiv rejected the query: %s", apiErr.Message)
		}
		return errs.Wrap(errs.KindGeneric, err, "arxiv returned an error: %s", apiErr.Message)
	}

	// The kind is added and the message is left alone. Wrapping with the error's
	// own text as the message would print the sentence twice, which is how
	// "2401.99999: not found: 2401.99999: not found" happens.
	switch {
	case errors.Is(err, ErrNotFound):
		return &errs.Error{Kind: errs.KindNotFound, Err: err}
	case errors.Is(err, context.DeadlineExceeded):
		return &errs.Error{Kind: errs.KindNetwork, Err: err}
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return &errs.Error{Kind: errs.KindNetwork, Err: err}
	}
	return err
}
