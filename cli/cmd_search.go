package cli

import (
	"github.com/spf13/cobra"
	"github.com/tamnd/arxiv-cli/arxiv"
)

func (a *App) searchCmd() *cobra.Command {
	var (
		category string
		sort     string
	)
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search arXiv papers",
		Long: `Search arXiv papers using the Open Access API.

The query is matched against all fields (title, abstract, authors, etc.).
Use --cat to restrict to a category code such as cs.LG or math.NT.
Use --sort to choose relevance (default), date (submitted), or updated.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n := a.effectiveLimit(10)
			opts := arxiv.SearchOptions{
				Query:    args[0],
				Category: category,
				Sort:     sort,
				Limit:    n,
			}
			a.progressf("searching for %q...", args[0])
			papers, err := a.client.Search(cmd.Context(), opts)
			if err != nil {
				return mapFetchErr(err)
			}
			return a.renderOrEmpty(papers, len(papers))
		},
	}
	cmd.Flags().StringVar(&category, "cat", "", "restrict to a category code (e.g. cs.LG, math.NT)")
	cmd.Flags().StringVar(&sort, "sort", "relevance", "sort order: relevance|date|updated")
	return cmd
}
