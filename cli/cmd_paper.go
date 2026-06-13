package cli

import (
	"github.com/spf13/cobra"
	"github.com/tamnd/arxiv-cli/arxiv"
)

func (a *App) paperCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "paper <id>",
		Short: "Fetch a single paper by arXiv id",
		Long: `Fetch a single paper by its arXiv id.

The id may be a bare id (1706.03762), a versioned id (1706.03762v7),
or a full URL (https://arxiv.org/abs/1706.03762). Version suffixes are
stripped so the latest revision is always returned.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := arxiv.ParsePaperID(args[0])
			if err != nil {
				return codeError(exitUsage, err)
			}
			a.progressf("fetching paper %s...", id)
			paper, err := a.client.Paper(cmd.Context(), id)
			if err != nil {
				return mapFetchErr(err)
			}
			return a.render([]arxiv.Paper{paper})
		},
	}
}
