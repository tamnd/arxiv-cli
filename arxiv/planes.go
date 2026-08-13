package arxiv

import (
	"fmt"
	"strings"
	"time"
)

// Measured is the date every number in this file was read off arXiv rather than
// assumed. It is printed by `arxiv planes` so the numbers carry their age.
const Measured = "2026-08-13"

// A Plane is a set of arXiv hosts that share a pace.
//
// arXiv runs two of them and they are not close: the API hosts answered
// eighteen requests in a row without a complaint, and arxiv.org itself asks for
// fifteen seconds between requests in robots.txt and means it. Pacing the two
// the same would either crawl the API pointlessly slowly or get the user agent
// blocked on the HTML side.
type Plane struct {
	// Name is the plane's name, used in messages and by `arxiv planes`.
	Name string `json:"name"`
	// Hosts are the hostnames that belong to it.
	Hosts []string `json:"hosts"`
	// Pace is the default minimum gap between requests to those hosts.
	Pace time.Duration `json:"pace"`
	// Floor is the smallest gap a flag is allowed to set.
	Floor time.Duration `json:"floor"`
	// Why is the evidence for the pace, printed by `arxiv planes` so the
	// number is a measurement rather than a preference.
	Why string `json:"why"`
}

// The two planes. Both paces were measured on the date in Measured.
var (
	// APIPlane is arXiv's machine-readable hosts.
	APIPlane = Plane{
		Name:  "api",
		Hosts: []string{"export.arxiv.org", "oaipmh.arxiv.org", "rss.arxiv.org"},
		Pace:  3 * time.Second,
		Floor: time.Second,
		Why:   "arXiv's terms of use ask for roughly one request every three seconds; eighteen requests at that pace drew no throttling",
	}

	// HTMLPlane is the website.
	HTMLPlane = Plane{
		Name:  "html",
		Hosts: []string{"arxiv.org", "www.arxiv.org"},
		Pace:  15 * time.Second,
		Floor: 15 * time.Second,
		Why:   "https://arxiv.org/robots.txt says Crawl-delay: 15; going faster returns HTTP 429 with a fourteen byte \"Rate exceeded.\" body and then stalls for about 45 seconds",
	}
)

// Planes is the table, in the order `arxiv planes` prints it.
var Planes = []Plane{APIPlane, HTMLPlane}

// PlaneFor returns the plane a host belongs to.
//
// The plane is chosen by host and never by the caller. A read that has to fall
// back from the API to the abstract page crosses planes halfway through, and if
// the pace travelled with the caller instead of the host that fallback would
// hit arxiv.org at the API's pace.
func PlaneFor(host string) (Plane, bool) { return planeIn(Planes, host) }

// planeIn is PlaneFor against a given table. A client carries its own table, so
// a test can pace a local server without editing package state.
func planeIn(planes []Plane, host string) (Plane, bool) {
	host = strings.ToLower(host)
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i] // strip a port, which test servers have
	}
	for _, p := range planes {
		for _, h := range p.Hosts {
			if h == host {
				return p, true
			}
		}
	}
	return Plane{}, false
}

// Clamp raises the plane's pace to d, refusing to lower it below the floor.
//
// The HTML floor is arXiv's own number and is not ours to lower. The API floor
// is looser because nothing was observed throttling there, and it is still a
// floor because an unthrottled loop is how a public service ends up writing a
// rule about you.
func (p Plane) Clamp(d time.Duration) (time.Duration, error) {
	if d <= 0 {
		return p.Pace, nil
	}
	if d < p.Floor {
		return 0, fmt.Errorf("%s", p.floorMessage(d))
	}
	return d, nil
}

// floorMessage explains a refused pace by quoting the evidence, so the answer
// to "why can I not go faster" is in the error rather than in the docs.
func (p Plane) floorMessage(d time.Duration) string {
	return fmt.Sprintf("%s is below the %s floor of %s: %s",
		d, p.Name, p.Floor, p.Why)
}
