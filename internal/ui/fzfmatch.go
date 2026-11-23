package ui

import (
	"math"
	"sort"
	"strings"

	"github.com/junegunn/fzf/src/algo"
	"github.com/junegunn/fzf/src/util"
)

// The path scheme gives fzf's boundary bonus to chars right after '/'. It is
// process-global in fzf, so every matcher in this package shares it.
func init() { algo.Init("path") }

type matchFn func(caseSensitive, normalize, forward bool, text *util.Chars, pattern []rune, withPos bool, slab *util.Slab) (algo.Result, *[]int)

// fzfTerm is one space-separated query term in fzf's extended-search syntax.
type fzfTerm struct {
	fn            matchFn
	pattern       []rune
	caseSensitive bool
	inverse       bool
}

// parseFzfQuery splits query like fzf's extended mode: terms are ANDed;
// 'exact, ^prefix, suffix$, ^equal$ and !negation (exact) are supported.
func parseFzfQuery(query string) []fzfTerm {
	var terms []fzfTerm
	for _, raw := range strings.Fields(query) {
		t := fzfTerm{fn: algo.FuzzyMatchV2}
		text := raw
		if strings.HasPrefix(text, "!") {
			t.inverse = true
			t.fn = algo.ExactMatchNaive
			text = text[1:]
		}
		switch {
		case strings.HasPrefix(text, "'"):
			t.fn = algo.ExactMatchNaive
			text = text[1:]
		case strings.HasPrefix(text, "^") && strings.HasSuffix(text, "$") && len(text) > 1:
			t.fn = algo.EqualMatch
			text = text[1 : len(text)-1]
		case strings.HasPrefix(text, "^"):
			t.fn = algo.PrefixMatch
			text = text[1:]
		case strings.HasSuffix(text, "$"):
			t.fn = algo.SuffixMatch
			text = text[:len(text)-1]
		}
		if text == "" {
			continue
		}
		// Smart case: any uppercase letter makes the term case-sensitive.
		t.caseSensitive = text != strings.ToLower(text)
		if !t.caseSensitive {
			text = strings.ToLower(text)
		}
		t.pattern = algo.NormalizeRunes([]rune(text))
		terms = append(terms, t)
	}
	return terms
}

// fzfMatch is one matching item: its index in the input, total score and
// pathname tiebreak (distance of the match start from the last '/').
type fzfMatch struct {
	index    int
	score    int
	pathname int
}

// fzfMatcher owns fzf's scratch slab; it is not safe for concurrent use.
type fzfMatcher struct {
	slab *util.Slab
}

func newFzfMatcher() *fzfMatcher {
	return &fzfMatcher{slab: util.MakeSlab(100*1024, 2048)}
}

// match returns the items that satisfy every term, ranked like fzf's path
// scheme (score, pathname, length), then input order.
func (fm *fzfMatcher) match(terms []fzfTerm, items []util.Chars) []fzfMatch {
	var out []fzfMatch
	for i := range items {
		if mt, ok := fm.score(terms, &items[i]); ok {
			mt.index = i
			out = append(out, mt)
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].score != out[b].score {
			return out[a].score > out[b].score
		}
		if out[a].pathname != out[b].pathname {
			return out[a].pathname < out[b].pathname
		}
		la, lb := items[out[a].index].Length(), items[out[b].index].Length()
		if la != lb {
			return la < lb
		}
		return out[a].index < out[b].index
	})
	return out
}

func (fm *fzfMatcher) score(terms []fzfTerm, text *util.Chars) (fzfMatch, bool) {
	var mt fzfMatch
	minBegin := -1
	for _, t := range terms {
		// withPos makes FuzzyMatchV2 backtrack, so res.Start is the real match
		// start the pathname tiebreak needs.
		res, _ := t.fn(t.caseSensitive, true, true, text, t.pattern, !t.inverse, fm.slab)
		if matched := res.Start >= 0; matched == t.inverse {
			return mt, false
		}
		mt.score += res.Score
		if !t.inverse && (minBegin < 0 || res.Start < minBegin) {
			minBegin = res.Start
		}
	}
	mt.pathname = math.MaxInt
	if minBegin >= 0 {
		lastDelim := -1
		for i := text.Length() - 1; i >= 0; i-- {
			if r := text.Get(i); r == '/' || r == '\\' {
				lastDelim = i
				break
			}
		}
		if lastDelim <= minBegin {
			mt.pathname = minBegin - lastDelim
		}
	}
	return mt, true
}

// matchPositions returns the rune offsets in text that the positive terms hit,
// for highlighting; it allocates, so call it only for visible rows.
func matchPositions(terms []fzfTerm, text string) map[int]bool {
	chars := util.ToChars([]byte(text))
	pos := make(map[int]bool)
	for _, t := range terms {
		if t.inverse {
			continue
		}
		res, p := t.fn(t.caseSensitive, true, true, &chars, t.pattern, true, nil)
		if res.Start < 0 {
			continue
		}
		if p != nil && len(*p) > 0 {
			for _, i := range *p {
				pos[i] = true
			}
			continue
		}
		// Exact/prefix/suffix matchers report a span rather than positions.
		for i := res.Start; i < res.End; i++ {
			pos[i] = true
		}
	}
	return pos
}
