package cli

import (
	"github.com/spf13/cobra"
	"github.com/tamnd/arxiv-cli/arxiv"
)

// commonCategories is the static list of the most-used arXiv category codes.
var commonCategories = []arxiv.Category{
	{Code: "cs.AI", Description: "Artificial Intelligence"},
	{Code: "cs.CL", Description: "Computation and Language"},
	{Code: "cs.CV", Description: "Computer Vision and Pattern Recognition"},
	{Code: "cs.DS", Description: "Data Structures and Algorithms"},
	{Code: "cs.GT", Description: "Computer Science and Game Theory"},
	{Code: "cs.IR", Description: "Information Retrieval"},
	{Code: "cs.IT", Description: "Information Theory"},
	{Code: "cs.LG", Description: "Machine Learning"},
	{Code: "cs.NE", Description: "Neural and Evolutionary Computing"},
	{Code: "cs.PL", Description: "Programming Languages"},
	{Code: "cs.RO", Description: "Robotics"},
	{Code: "cs.SE", Description: "Software Engineering"},
	{Code: "cs.SY", Description: "Systems and Control"},
	{Code: "econ.EM", Description: "Econometrics"},
	{Code: "econ.GN", Description: "General Economics"},
	{Code: "math.CO", Description: "Combinatorics"},
	{Code: "math.NT", Description: "Number Theory"},
	{Code: "math.OC", Description: "Optimization and Control"},
	{Code: "math.PR", Description: "Probability"},
	{Code: "math.ST", Description: "Statistics Theory"},
	{Code: "physics.acc-ph", Description: "Accelerator Physics"},
	{Code: "physics.data-an", Description: "Data Analysis, Statistics and Probability"},
	{Code: "q-bio.GN", Description: "Genomics"},
	{Code: "q-bio.NC", Description: "Neurons and Cognition"},
	{Code: "q-bio.QM", Description: "Quantitative Methods"},
	{Code: "quant-ph", Description: "Quantum Physics"},
	{Code: "stat.AP", Description: "Applications"},
	{Code: "stat.ML", Description: "Machine Learning (Statistics)"},
	{Code: "stat.ME", Description: "Methodology"},
	{Code: "stat.TH", Description: "Statistics Theory (alias for math.ST)"},
}

func (a *App) categoriesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "categories",
		Short: "List common arXiv category codes",
		Long:  "Print the most-used arXiv category codes. No network call.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.render(commonCategories)
		},
	}
}
