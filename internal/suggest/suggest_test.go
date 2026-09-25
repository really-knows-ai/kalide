package suggest

import "testing"

func TestClosest(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		candidates []string
		want       string
	}{
		{
			name:       "close match is suggested",
			input:      "titel",
			candidates: []string{"title", "author", "date"},
			want:       "title",
		},
		{
			name:       "smallest distance wins over lexicographic order",
			input:      "titel",
			candidates: []string{"author", "date", "title"},
			want:       "title",
		},
		{
			name:       "name is compared case-insensitively",
			input:      "TITLE",
			candidates: []string{"title"},
			want:       "title",
		},
		{
			name:       "mixed-case input matches lower-case candidate",
			input:      "TiTlE",
			candidates: []string{"title"},
			want:       "title",
		},
		{
			name:       "candidate is returned in its original spelling",
			input:      "titel",
			candidates: []string{"TITLE"},
			want:       "TITLE",
		},
		{
			name:       "exact match is returned",
			input:      "title",
			candidates: []string{"author", "title"},
			want:       "title",
		},
		{
			name:       "tie broken by lexicographically smallest candidate",
			input:      "title",
			candidates: []string{"titlb", "titla"},
			want:       "titla",
		},
		{
			name:       "tie break independent of candidate order",
			input:      "title",
			candidates: []string{"titla", "titlb"},
			want:       "titla",
		},
		{
			name:       "tie break picks smallest even when listed last",
			input:      "cat",
			candidates: []string{"cut", "cot"},
			want:       "cot",
		},
		{
			name:       "tie break order permutation yields same result",
			input:      "cat",
			candidates: []string{"cot", "cut"},
			want:       "cot",
		},
		{
			name:       "no suggestion when nothing is close",
			input:      "zzzzzz",
			candidates: []string{"title", "author", "date"},
			want:       "",
		},
		{
			name:       "single-edit threshold met for one-letter name",
			input:      "a",
			candidates: []string{"ab"},
			want:       "ab",
		},
		{
			name:       "one-letter name never matches two edits away",
			input:      "a",
			candidates: []string{"bc"},
			want:       "",
		},
		{
			name:       "threshold capped at three edits",
			input:      "abcdefghij",
			candidates: []string{"abcdefg"},
			want:       "abcdefg",
		},
		{
			name:       "four edits away is not suggested despite cap",
			input:      "abcdefghij",
			candidates: []string{"abcdef"},
			want:       "",
		},
		{
			name:       "empty name yields no suggestion",
			input:      "",
			candidates: []string{"title"},
			want:       "",
		},
		{
			name:       "no candidates yields no suggestion",
			input:      "titel",
			candidates: nil,
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Closest(tt.input, tt.candidates)
			if got != tt.want {
				t.Fatalf("Closest(%q, %q) = %q, want %q", tt.input, tt.candidates, got, tt.want)
			}
		})
	}
}
