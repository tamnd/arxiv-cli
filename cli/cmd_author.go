package cli

import (
	"github.com/spf13/cobra"
)

func (a *App) authorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "author <name>",
		Short: "List papers by an author",
		Long: `List papers by an author name.

The name is passed to the arXiv au: field prefix. Surname-only and
full-name queries both work. Results are sorted newest first.

Examples:
  arxiv author Vaswani
  arxiv author "Yann LeCun" -n 20`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n := a.effectiveLimit(10)
			a.progressf("searching papers by %q...", args[0])
			papers, err := a.client.SearchByAuthor(cmd.Context(), args[0], n)
			if err != nil {
				return mapFetchErr(err)
			}
			return a.renderOrEmpty(papers, len(papers))
		},
	}
}
