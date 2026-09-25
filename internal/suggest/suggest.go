// Package suggest provides a closest-match helper for author-facing
// "did you mean …?" error hints.
//
// It is shared by the config loader, template/section schema checks and link
// resolution: callers pass the offending name and the set of known names, and
// get back the closest candidate when one is close enough, or "" when none is.
package suggest

import "strings"

// Closest returns the candidate closest to name, or "" when no candidate is
// close enough. Comparison is case-insensitive; the returned value is the
// candidate in its original spelling, so callers can quote it verbatim.
//
// Distance is Levenshtein edit distance. A candidate is close enough when its
// distance is at most ceil(len(name)/3), capped at 3 and never below 1. Ties
// are broken deterministically by choosing the lexicographically smallest
// candidate, independent of the order of candidates.
func Closest(name string, candidates []string) string {
	if name == "" || len(candidates) == 0 {
		return ""
	}

	lower := strings.ToLower(name)
	limit := (len(lower) + 2) / 3
	if limit < 1 {
		limit = 1
	}
	if limit > 3 {
		limit = 3
	}

	best := ""
	bestDist := limit + 1
	for _, candidate := range candidates {
		d := levenshtein(lower, strings.ToLower(candidate))
		if d > limit {
			continue
		}
		if d < bestDist {
			best, bestDist = candidate, d
			continue
		}
		if d == bestDist && candidate < best {
			best = candidate
		}
	}
	return best
}

// levenshtein returns the Levenshtein edit distance between a and b. Its
// inputs are expected to already be lower-cased.
func levenshtein(a, b string) int {
	ar, br := []rune(a), []rune(b)
	if len(ar) == 0 {
		return len(br)
	}
	if len(br) == 0 {
		return len(ar)
	}

	prev := make([]int, len(br)+1)
	curr := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		curr[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(br)]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
