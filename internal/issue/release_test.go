package issue

import (
	"testing"
	"time"
)

func TestReleaseNewer(t *testing.T) {
	seen := map[string]time.Time{"build-1021": time.Unix(1000, 0), "build-1022": time.Unix(2000, 0), "canary": time.Unix(3000, 0)}
	fs := func(r string) (time.Time, bool) { t, ok := seen[r]; return t, ok }
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.10.0", "1.9.0", true},
		{"1.9.0", "1.10.0", false},
		{"v2.0.0", "1.99.99", true},
		{"2.0.0", "2.0.0", false},
		{"2.0.0", "2.0.0-rc.1", true},
		{"2.0.0-rc.2", "2.0.0-rc.1", true},
		{"2.0.0-rc.1", "2.0.0", false},
		{"2.0.0+build.7", "2.0.0", false},
		{"2.0", "2.0.0", false},
		{"2.0.1", "2.0", true},
		{"build-1022", "build-1021", true},
		{"build-1021", "build-1022", false},
		{"canary", "nope", false}, // resolved-in release never shipped: nothing to compare
		{"fresh", "canary", true}, // first sighting of an opaque release after the resolved-in one
		{"nope", "never", false},  // neither known
		{"", "1.0", false},
		{"1.0", "", false},
		{"2.0.0", "build-1021", true}, // mixed: semver vs opaque; 2.0.0 never seen, build-1021 was
		{"build-1021", "2.0.0", false},
	}
	for _, c := range cases {
		if got := ReleaseNewer(c.a, c.b, fs); got != c.want {
			t.Errorf("ReleaseNewer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
	if ReleaseNewer("build-1022", "build-1021", nil) {
		t.Fatal("without a first-seen source opaque releases are never newer")
	}
}
