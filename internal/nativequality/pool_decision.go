package nativequality

import ()

func samePoolMembers(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]bool, len(a))
	for _, tag := range a {
		if seen[tag] {
			return false
		}
		seen[tag] = true
	}
	for _, tag := range b {
		if !seen[tag] {
			return false
		}
		delete(seen, tag)
	}
	return len(seen) == 0
}

// poolDecision applies the nightly rule (revision 2026-10-11): the ranked
// best six of the measured nodes replace the pool when the membership
// differs. Fewer than six valid results never shrink a larger running pool of
// live members, so a bad night keeps what works.
func poolDecision(active, orphans, selected []string, firstInitialization bool) string {
	if samePoolMembers(active, selected) {
		return "pool-unchanged"
	}
	live := len(active) - len(orphans)
	if firstInitialization {
		live = 6
	}
	if len(selected) < 6 && len(selected) < live {
		return "insufficient-valid-results"
	}
	return "best-six"
}
