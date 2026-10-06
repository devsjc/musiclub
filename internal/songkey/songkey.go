// Package songkey normalises artist and title into a key for duplicate
// detection (DESIGN.md §5.3). It is pure: no I/O, no dependencies on the rest
// of the app.
package songkey

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// noise is version information that doesn't make a different song: remaster
// notes, single/album/radio versions and the like. Remixes, live versions and
// covers are deliberately not here.
const noise = `(?:\d{4}\s+)?(?:remaster(?:ed)?(?:\s+\d{4})?|digital remaster|(?:single|album|radio|mono|stereo|original)(?:\s+(?:version|edit|mix))?|(?:deluxe|expanded|anniversary)(?:\s+edition)?|bonus track|explicit)`

var (
	featPart    = `(?:feat\.?|ft\.?|featuring)\s`
	bracketed   = regexp.MustCompile(`\s*[(\[]([^()\[\]]*)[)\]]`)
	bracketNoop = regexp.MustCompile(`(?i)^\s*(?:` + featPart + `.*|` + noise + `(?:\s*[,/&]\s*` + noise + `)*)\s*$`)
	dashSuffix  = regexp.MustCompile(`(?i)\s[-–—]\s+(` + noise + `)\s*$`)
	trailingFt  = regexp.MustCompile(`(?i)\s` + featPart + `.*$`)

	// Accents are removed (é -> e) but other combining marks, like the Japanese
	// dakuten that makes ば a different letter from は, are kept.
	accents = runes.Predicate(func(r rune) bool { return r >= 0x300 && r <= 0x36f })
	folder  = transform.Chain(norm.NFKC, norm.NFD, runes.Remove(accents), norm.NFC)
	extra   = strings.NewReplacer("ø", "o", "æ", "ae", "œ", "oe", "ß", "ss", "đ", "d", "ł", "l", "ı", "i", "&", " and ")
)

// Key returns the duplicate-detection key for a song: "artist|title", each
// part lower-cased, accent-folded, stripped of punctuation and of noise such as
// "(feat. X)", "(Remastered 2011)" or " - Single Version". The key is empty if
// either part has no letters or digits left.
func Key(artist, title string) string {
	a, t := part(artist), part(title)
	if a == "" || t == "" {
		return ""
	}
	return a + "|" + t
}

func part(s string) string {
	stripped := trailingFt.ReplaceAllString(dashSuffix.ReplaceAllString(dropBracketed(s), ""), "")
	// If stripping left nothing (a song actually called "(Remastered)"), keep the original.
	if f := fold(stripped); f != "" {
		return f
	}
	return fold(s)
}

func dropBracketed(s string) string {
	return bracketed.ReplaceAllStringFunc(s, func(m string) string {
		inner := bracketed.FindStringSubmatch(m)[1]
		if bracketNoop.MatchString(inner) {
			return ""
		}
		return m
	})
}

func fold(s string) string {
	s = strings.ToLower(s)
	s, _, _ = transform.String(folder, s)
	s = extra.Replace(s)
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\'' || r == '’' || r == '`':
			return -1 // "don't" -> "dont"
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			return r
		}
		return ' '
	}, s)
	return strings.Join(strings.Fields(s), " ")
}
