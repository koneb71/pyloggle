package version

import "testing"

func TestReleaseVersionWins(t *testing.T) {
	old, oldCommit := Version, Commit
	defer func() { Version, Commit = old, oldCommit }()
	Version, Commit = "1.0.0", "abc1234"
	if Get() != "1.0.0" || String() != "1.0.0 (abc1234)" {
		t.Fatalf("got %q / %q", Get(), String())
	}
	Version, Commit = "dev", ""
	if Get() == "" {
		t.Fatal("an unset version still reports something")
	}
}
