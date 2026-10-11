package issue

import (
	"strconv"
	"strings"
	"time"
)

// ReleaseNewer reports whether release a is newer than release b. Both parse as loose semantic
// versions (optional v prefix, dotted numbers, optional -prerelease, +build ignored) → numeric
// comparison; otherwise the releases' first-seen times decide. A release b that has been seen
// and an a that never has is "a is newer": the caller holds a's first occurrence, which has not
// reached the releases table yet. Any other unknown → not newer. Plain string comparison is
// wrong ("1.10" < "1.9") and first-seen alone misorders backported builds.
func ReleaseNewer(a, b string, firstSeen func(string) (time.Time, bool)) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if a == "" || b == "" || a == b {
		return false
	}
	if va, ok := parseVersion(a); ok {
		if vb, ok := parseVersion(b); ok {
			return compareVersions(va, vb) > 0
		}
	}
	if firstSeen != nil {
		ta, oka := firstSeen(a)
		tb, okb := firstSeen(b)
		switch {
		case oka && okb:
			return ta.After(tb)
		case okb && !oka:
			return true // first sighting of a, after b's
		}
	}
	return false
}

type version struct {
	nums []int
	pre  string
}

func parseVersion(s string) (version, bool) {
	s = strings.TrimSpace(s)
	if len(s) > 1 && (s[0] == 'v' || s[0] == 'V') && s[1] >= '0' && s[1] <= '9' {
		s = s[1:]
	}
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	pre := ""
	if i := strings.IndexByte(s, '-'); i >= 0 {
		s, pre = s[:i], s[i+1:]
	}
	parts := strings.Split(s, ".")
	if len(parts) == 0 || len(parts) > 5 {
		return version{}, false
	}
	v := version{pre: pre}
	for _, p := range parts {
		if p == "" || len(p) > 9 {
			return version{}, false
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return version{}, false
		}
		v.nums = append(v.nums, n)
	}
	return v, true
}

func compareVersions(a, b version) int {
	n := len(a.nums)
	if len(b.nums) > n {
		n = len(b.nums)
	}
	for i := 0; i < n; i++ {
		var x, y int
		if i < len(a.nums) {
			x = a.nums[i]
		}
		if i < len(b.nums) {
			y = b.nums[i]
		}
		if x != y {
			if x > y {
				return 1
			}
			return -1
		}
	}
	switch {
	case a.pre == "" && b.pre != "":
		return 1
	case a.pre != "" && b.pre == "":
		return -1
	}
	return strings.Compare(a.pre, b.pre)
}
