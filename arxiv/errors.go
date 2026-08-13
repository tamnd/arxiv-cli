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
	switch {
	case errors.Is(err, ErrNotFound):
		return errs.Wrap(errs.KindNotFound, err, "%s", err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		return errs.Wrap(errs.KindNetwork, err, "%s", err.Error())
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return errs.Wrap(errs.KindNetwork, err, "%s", err.Error())
	}
	return err
}
