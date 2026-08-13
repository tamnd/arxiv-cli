package arxiv

// Category is the record `arxiv categories` prints. The taxonomy read that
// fills in the group, the archive and the set spec is milestone 9.
type Category struct {
	Code        string `json:"code" kit:"id" table:"code"`
	Description string `json:"description" table:"description"`
}

// commonCategories is a static subset of the arXiv category codes.
//
// arXiv publishes all 155 of them, with descriptions, at
// https://arxiv.org/category_taxonomy. Reading that page is milestone 7 of the
// rewrite; until it lands this list is what `arxiv categories` prints, and it
// is a fifth of the real thing.
var commonCategories = []Category{
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
	{Code: "stat.ME", Description: "Methodology"},
	{Code: "stat.ML", Description: "Machine Learning (Statistics)"},
	{Code: "stat.TH", Description: "Statistics Theory (alias for math.ST)"},
}

// CommonCategories returns the category codes the tool knows about.
func CommonCategories() []Category {
	out := make([]Category, len(commonCategories))
	copy(out, commonCategories)
	return out
}
