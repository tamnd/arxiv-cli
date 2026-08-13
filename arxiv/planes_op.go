package arxiv

import (
	"context"
	"strings"

	"github.com/tamnd/any-cli/kit"
)

// PlaneInfo is one row of the pacing table as the command prints it.
//
// The durations are strings rather than time.Duration because a duration
// marshals to a nanosecond count, and "15s" is the thing the reader wants next
// to a flag that takes "15s".
type PlaneInfo struct {
	Name  string   `json:"name" kit:"id" table:"plane"`
	Hosts []string `json:"hosts" table:"-"`
	// HostList is the same hosts joined, for the table alone. The renderer
	// prints a list as its length, and "3" is not what anyone came to read.
	HostList string `json:"-" table:"hosts"`
	Pace     string `json:"pace" table:"pace"`
	Floor    string `json:"floor" table:"floor"`
	Flag     string `json:"flag" table:"flag"`
	Measured string `json:"measured" table:"-"`
	Why      string `json:"why" table:"-"`
}

// planeInfos renders the table.
func planeInfos() []PlaneInfo {
	out := make([]PlaneInfo, 0, len(Planes))
	for _, p := range Planes {
		flag := "--rate"
		if p.Name == HTMLPlane.Name {
			flag = "--" + htmlRateFlag
		}
		out = append(out, PlaneInfo{
			Name:     p.Name,
			Hosts:    p.Hosts,
			HostList: strings.Join(p.Hosts, ", "),
			Pace:     p.Pace.String(),
			Floor:    p.Floor.String(),
			Flag:     flag,
			Measured: Measured,
			Why:      p.Why,
		})
	}
	return out
}

type planesIn struct{}

func registerPlanes(app *kit.App) {
	kit.Handle(app, kit.OpMeta{
		Name:    "planes",
		Group:   "explain",
		List:    true,
		URIType: "plane",
		Summary: "Show the pace this tool keeps and why",
		Long: `Show the two paces this tool keeps and the evidence for each.

arXiv runs two sets of hosts and they want to be treated differently. The API
hosts serve machine-readable formats and take a request every few seconds. The
website asks for fifteen seconds between requests in its robots.txt and enforces
it, so both paces exist and --rate only moves the first one.

Every number here was measured on the date in the measured field, and the why
field says what was measured. No network call.`,
	}, func(_ context.Context, _ planesIn, emit func(*PlaneInfo) error) error {
		return emitAll(planeInfos(), emit)
	})
}
