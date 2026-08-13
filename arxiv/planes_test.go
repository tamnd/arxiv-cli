package arxiv

import (
	"strings"
	"testing"
	"time"
)

func TestPlaneForHost(t *testing.T) {
	cases := []struct {
		host string
		want string
		ok   bool
	}{
		{"export.arxiv.org", "api", true},
		{"oaipmh.arxiv.org", "api", true},
		{"rss.arxiv.org", "api", true},
		{"arxiv.org", "html", true},
		{"www.arxiv.org", "html", true},
		{"ARXIV.ORG", "html", true},
		{"arxiv.org:443", "html", true},
		{"doi.org", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		p, ok := PlaneFor(tc.host)
		if ok != tc.ok {
			t.Errorf("PlaneFor(%q) ok = %v, want %v", tc.host, ok, tc.ok)
			continue
		}
		if p.Name != tc.want {
			t.Errorf("PlaneFor(%q) = %q, want %q", tc.host, p.Name, tc.want)
		}
	}
}

// TestHTMLFloorIsRobots pins the HTML pace to the number arXiv publishes. If
// robots.txt ever changes this test is the place that has to be edited on
// purpose, rather than a constant somebody nudges.
func TestHTMLFloorIsRobots(t *testing.T) {
	if HTMLPlane.Floor != 15*time.Second {
		t.Errorf("html floor = %s, want 15s from robots.txt Crawl-delay", HTMLPlane.Floor)
	}
	if HTMLPlane.Pace < HTMLPlane.Floor {
		t.Errorf("html pace %s is under its own floor %s", HTMLPlane.Pace, HTMLPlane.Floor)
	}
	if !strings.Contains(HTMLPlane.Why, "Crawl-delay") {
		t.Errorf("html why does not cite robots.txt: %q", HTMLPlane.Why)
	}
}

func TestClamp(t *testing.T) {
	cases := []struct {
		plane Plane
		in    time.Duration
		want  time.Duration
		fails bool
	}{
		{APIPlane, 0, APIPlane.Pace, false},
		{APIPlane, -time.Second, APIPlane.Pace, false},
		{APIPlane, time.Second, time.Second, false},
		{APIPlane, 10 * time.Second, 10 * time.Second, false},
		{APIPlane, 500 * time.Millisecond, 0, true},
		{HTMLPlane, 15 * time.Second, 15 * time.Second, false},
		{HTMLPlane, time.Minute, time.Minute, false},
		{HTMLPlane, 14 * time.Second, 0, true},
		{HTMLPlane, time.Second, 0, true},
	}
	for _, tc := range cases {
		got, err := tc.plane.Clamp(tc.in)
		if tc.fails {
			if err == nil {
				t.Errorf("%s.Clamp(%s) = %s, want an error", tc.plane.Name, tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s.Clamp(%s): %v", tc.plane.Name, tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s.Clamp(%s) = %s, want %s", tc.plane.Name, tc.in, got, tc.want)
		}
	}
}

// TestFloorErrorCarriesEvidence checks that a refused pace explains itself. A
// bare "too fast" sends the user to the source; the number and the reason in the
// message answer it where it was asked.
func TestFloorErrorCarriesEvidence(t *testing.T) {
	_, err := HTMLPlane.Clamp(time.Second)
	if err == nil {
		t.Fatal("expected an error for 1s on the html plane")
	}
	msg := err.Error()
	for _, want := range []string{"1s", "15s", "robots.txt"} {
		if !strings.Contains(msg, want) {
			t.Errorf("floor error %q does not mention %q", msg, want)
		}
	}
}

func TestNewClientRefusesSubFloorPace(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HTMLRate = time.Second
	if _, err := NewClient(cfg); err == nil {
		t.Fatal("expected NewClient to refuse --html-rate 1s")
	} else if !strings.Contains(err.Error(), "--html-rate") {
		t.Errorf("error %q does not name the flag", err)
	}

	cfg = DefaultConfig()
	cfg.Rate = 100 * time.Millisecond
	if _, err := NewClient(cfg); err == nil {
		t.Fatal("expected NewClient to refuse --rate 100ms")
	} else if !strings.Contains(err.Error(), "--rate") {
		t.Errorf("error %q does not name the flag", err)
	}
}

// TestPlaneInfosMatchTable checks the printed table against the table the client
// actually paces by, so `arxiv planes` cannot drift into a nice story.
func TestPlaneInfosMatchTable(t *testing.T) {
	rows := planeInfos()
	if len(rows) != len(Planes) {
		t.Fatalf("planeInfos has %d rows, want %d", len(rows), len(Planes))
	}
	for i, row := range rows {
		p := Planes[i]
		if row.Name != p.Name {
			t.Errorf("row %d name = %q, want %q", i, row.Name, p.Name)
		}
		if row.Pace != p.Pace.String() {
			t.Errorf("row %s pace = %q, want %q", row.Name, row.Pace, p.Pace)
		}
		if row.Floor != p.Floor.String() {
			t.Errorf("row %s floor = %q, want %q", row.Name, row.Floor, p.Floor)
		}
		if row.Measured != Measured {
			t.Errorf("row %s measured = %q, want %q", row.Name, row.Measured, Measured)
		}
		if row.Why == "" {
			t.Errorf("row %s has no evidence", row.Name)
		}
	}
	if rows[0].Flag != "--rate" || rows[1].Flag != "--html-rate" {
		t.Errorf("flags = %q, %q; want --rate, --html-rate", rows[0].Flag, rows[1].Flag)
	}
}
