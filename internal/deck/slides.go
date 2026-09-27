package deck

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// SlidesDir is the fixed name of the slide directory at the root of a deck.
// Slide files are named <number>[letter]-<label>.md (slide-file-ordering).
const SlidesDir = "slides"

// slideName matches a slide filename: <number>[letter]-<label>.md. The single
// optional letter makes the slide a reveal.js vertical slide under its numbered
// slide; the number is a run of digits and the label is the anchor id.
var slideName = regexp.MustCompile(`^([0-9]+)([A-Za-z]?)-([A-Za-z0-9_-]+)\.md$`)

// Slide is one parsed slide file: the identity and position derived from its
// filename. Its contents are read by the slide parser, not here.
type Slide struct {
	// Path is the slide file's path within the deck filesystem, e.g.
	// "slides/3-team.md". It is the slide's file identity and what errors
	// name. A filename has no interior line, so filename errors are
	// positioned by path only.
	Path string

	// Number is the numeric position (slide-file-ordering). Stacks are
	// ordered by it numerically, so 10 follows 9; gaps are allowed.
	Number int

	// Letter is the vertical suffix for a reveal.js vertical slide, or ""
	// for a horizontal slide. It is lower-cased.
	Letter string

	// Label is the text after the dash, and becomes the slide's anchor id. It
	// is unique across the deck (unique-slide-labels).
	Label string
}

// PositionLabel returns the slide's position label: the number with the
// lower-cased letter appended for a vertical (letter) slide, and no letter for
// a horizontal slide (slide-position). It is neither a flattened presentation
// index nor the bare filename integer.
func (s Slide) PositionLabel() string {
	return strconv.Itoa(s.Number) + s.Letter
}

// Stack is one horizontal slide together with the vertical slides revealed
// beneath it. Vertical slides nest exactly one level: a letter slide is always
// a child of its numbered slide.
type Stack struct {
	// Slide is the numbered horizontal slide.
	Slide Slide

	// Vertical holds the letter slides beneath Slide, ordered by letter.
	Vertical []Slide
}

// Deck is a deck's slides: the horizontal slides in numeric order, each with
// its ordered vertical slides. It is produced by LoadSlides.
type Deck struct {
	// Stacks is the ordered horizontal slides.
	Stacks []Stack
}

// Total returns the number of slide files in the deck: every horizontal stack
// slide plus its vertical slides. It is derived at render time from the deck
// model, never read from frontmatter (slide-metadata).
func (d Deck) Total() int {
	total := 0
	for _, stack := range d.Stacks {
		total += 1 + len(stack.Vertical)
	}
	return total
}

// positionKey identifies a slide's (number, letter) position; two slides may
// not share one (domain.constraint.deck-slide-identity).
type positionKey struct {
	number int
	letter string
}

// LoadSlides lists and parses the slide files in dir within fsys — typically
// SlidesDir at the root of a deck directory — and returns them as an ordered
// deck model.
//
// Rules:
//   - every regular file must be named <number>[letter]-<label>.md; any other
//     filename is an error (slide-file-ordering);
//   - slides are ordered numerically (10 after 9); gaps are allowed;
//   - two slides with the same (number, letter) position are an error — a
//     duplicate number for horizontal slides, a duplicate number+letter for
//     vertical ones (deck-slide-identity);
//   - a letter slide is a one-level vertical slide under its numbered slide; a
//     letter slide whose numbered slide is absent is an error (vertical-slides);
//   - labels are unique across the deck and become anchor ids; a duplicate
//     label is an error naming both files (unique-slide-labels).
//
// Nested directory entries are ignored. Errors that name a specific file are
// positioned by path only, since a filename has no line.
func LoadSlides(fsys fs.FS, dir string) (*Deck, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", dir, err)
	}

	slides := make([]Slide, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		file := path.Join(dir, entry.Name())
		s, err := parseSlideFilename(entry.Name())
		if err != nil {
			return nil, positioned(file, 0, "%v", err)
		}
		s.Path = file
		slides = append(slides, s)
	}
	sort.Slice(slides, func(i, j int) bool { return slideBefore(slides[i], slides[j]) })

	// Duplicate positions: the same number for horizontal slides, the same
	// number+letter for vertical ones.
	position := make(map[positionKey]Slide, len(slides))
	for _, s := range slides {
		key := positionKey{number: s.Number, letter: s.Letter}
		if prev, ok := position[key]; ok {
			if s.Letter == "" {
				return nil, positioned(s.Path, 0, "duplicate slide number %d (also in %s)", s.Number, prev.Path)
			}
			return nil, positioned(s.Path, 0, "duplicate vertical slide %d%s (also in %s)", s.Number, s.Letter, prev.Path)
		}
		position[key] = s
	}

	// Build the horizontal stacks, then attach each letter slide from the same
	// number. A letter slide without its numbered slide is an orphan.
	deck := &Deck{}
	stackIndex := make(map[int]int, len(slides))
	for _, s := range slides {
		if s.Letter != "" {
			continue
		}
		stackIndex[s.Number] = len(deck.Stacks)
		deck.Stacks = append(deck.Stacks, Stack{Slide: s})
	}
	for _, s := range slides {
		if s.Letter == "" {
			continue
		}
		i, ok := stackIndex[s.Number]
		if !ok {
			return nil, positioned(s.Path, 0, "vertical slide %d%s has no numbered slide %d", s.Number, s.Letter, s.Number)
		}
		deck.Stacks[i].Vertical = append(deck.Stacks[i].Vertical, s)
	}

	// Labels are deck-unique anchors; on a duplicate report the later file and
	// name both.
	label := make(map[string]Slide, len(slides))
	for _, s := range slides {
		if prev, ok := label[s.Label]; ok {
			return nil, positioned(s.Path, 0, "duplicate label %q (also in %s)", s.Label, prev.Path)
		}
		label[s.Label] = s
	}
	return deck, nil
}

// parseSlideFilename parses a bare slide filename into its position and label,
// without the directory. It returns an error for a name that does not match
// <number>[letter]-<label>.md.
func parseSlideFilename(name string) (Slide, error) {
	m := slideName.FindStringSubmatch(name)
	if m == nil {
		return Slide{}, errors.New("filename must be <number>[letter]-<label>.md")
	}
	number, err := strconv.Atoi(m[1])
	if err != nil {
		return Slide{}, fmt.Errorf("slide number %q is too large", m[1])
	}
	return Slide{Number: number, Letter: strings.ToLower(m[2]), Label: m[3]}, nil
}

// slideBefore orders slides by numeric position, then letter, then path, so
// duplicate detection and the resulting model are deterministic. The empty
// letter (a horizontal slide) sorts before its vertical children.
func slideBefore(a, b Slide) bool {
	if a.Number != b.Number {
		return a.Number < b.Number
	}
	if a.Letter != b.Letter {
		return a.Letter < b.Letter
	}
	return a.Path < b.Path
}
