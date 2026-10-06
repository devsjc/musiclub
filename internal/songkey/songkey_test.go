package songkey

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type song struct{ artist, title string }

func TestKeyEquivalent(t *testing.T) {
	for _, group := range [][]song{
		{{"Radiohead", "Karma Police"}, {"radiohead", "KARMA POLICE"}, {"  Radiohead ", "Karma   Police "}, {"Radiohead", "Karma Police!"}},
		{{"Beyoncé", "Halo"}, {"Beyonce", "Halo"}},
		{{"Sigur Rós", "Hoppípolla"}, {"Sigur Ros", "Hoppipolla"}},
		{{"Guns N' Roses", "Don't Cry"}, {"Guns N Roses", "Dont Cry"}, {"Guns N’ Roses", "Don’t Cry"}},
		{{"Simon & Garfunkel", "The Boxer"}, {"Simon and Garfunkel", "The Boxer"}},
		{{"Bowie", "Heroes"}, {"Bowie", "Heroes (2017 Remaster)"}, {"Bowie", "Heroes - Remastered 2017"}, {"Bowie", "Heroes [Remastered]"}, {"Bowie", "Heroes - 2017 Remaster"}},
		{{"Queen", "Bohemian Rhapsody"}, {"Queen", "Bohemian Rhapsody (Remastered 2011)"}, {"Queen", "Bohemian Rhapsody - Single Version"}, {"Queen", "Bohemian Rhapsody (Remastered 2011) [Mono]"}},
		{{"Daft Punk", "Get Lucky"}, {"Daft Punk feat. Pharrell Williams", "Get Lucky (feat. Pharrell Williams)"}, {"Daft Punk", "Get Lucky (ft. Pharrell Williams & Nile Rodgers)"}, {"Daft Punk", "Get Lucky feat. Pharrell"}, {"Daft Punk", "Get Lucky (Radio Edit)"}},
		{{"ABBA", "Dancing Queen - Single Version"}, {"Abba", "Dancing Queen (Album Version)"}},
		{{"Sigur Rós", "Svefn-g-englar"}, {"Sigur Ros", "Svefn g englar"}},
		{{"Björk", "Jóga"}, {"Bjork", "Joga"}},
		{{"Mötley Crüe", "Kickstart My Heart"}, {"Motley Crue", "Kickstart My Heart"}},
		{{"Sigur Rós", "Ágætis byrjun"}, {"Sigur Ros", "Agaetis byrjun"}, {"Sigur Ros", "Ágætis Byrjun"}},
	} {
		want := Key(group[0].artist, group[0].title)
		require.NotEmpty(t, want, "Key(%v)", group[0])
		for _, s := range group[1:] {
			require.Equal(t, want, Key(s.artist, s.title), "Key(%q, %q) should match %v", s.artist, s.title, group[0])
		}
	}
}

func TestKeyDistinct(t *testing.T) {
	for _, pair := range [][2]song{
		{{"Radiohead", "Karma Police"}, {"Radiohead", "Creep"}},
		{{"Radiohead", "Creep"}, {"Stone Temple Pilots", "Creep"}},
		{{"Daft Punk", "Around the World"}, {"Daft Punk", "Around the World (Remix)"}},
		{{"Nirvana", "Come as You Are"}, {"Nirvana", "Come as You Are (Live)"}},
		{{"Nirvana", "Polly"}, {"Nirvana", "Polly (Acoustic)"}},
		{{"Cher", "Believe"}, {"Cher", "Believe (feat. Someone) (Remix)"}},
		{{"Artist", "Song (Part 1)"}, {"Artist", "Song (Part 2)"}},
		{{"Artist", "Defeat"}, {"Artist", "De"}},
		{{"A|B", "C"}, {"A", "B|C"}},
	} {
		a, b := Key(pair[0].artist, pair[0].title), Key(pair[1].artist, pair[1].title)
		require.NotEqual(t, a, b, "Key(%v) and Key(%v) should differ", pair[0], pair[1])
	}
}

func TestKeyShape(t *testing.T) {
	cases := []struct {
		artist, title, want string
	}{
		{"Radiohead", "Karma Police", "radiohead|karma police"},
		{"Simon & Garfunkel", "The Boxer", "simon and garfunkel|the boxer"},
		{"Guns N' Roses", "Don't Cry", "guns n roses|dont cry"},
		{"Ke$ha", "Tik Tok", "ke ha|tik tok"},
		{"Måneskin", "Zitti e buoni", "maneskin|zitti e buoni"},
		{"くるり", "ばらの花", "くるり|ばらの花"},
		{"Artist", "(Remastered)", "artist|remastered"}, // a title that is all noise is kept
		{"Artist", "99 Problems", "artist|99 problems"},
	}
	for _, c := range cases {
		require.Equal(t, c.want, Key(c.artist, c.title), "Key(%q, %q)", c.artist, c.title)
	}
}

func TestKeyEmpty(t *testing.T) {
	for _, s := range []song{{"", "x"}, {"x", ""}, {"???", "x"}, {"x", "!!!"}, {" ", " "}} {
		require.Empty(t, Key(s.artist, s.title), "Key(%q, %q)", s.artist, s.title)
	}
}
