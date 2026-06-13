package cli

import (
	"errors"

	"github.com/tamnd/arxiv-cli/arxiv"
)

func isNotFound(err error) bool {
	return errors.Is(err, arxiv.ErrNotFound)
}
