package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
)

// sessions signs and verifies the "who am I" cookie value (DESIGN.md §8.4).
// The value is "<playerID>.<mac>", where the MAC covers the club slug too, so a
// cookie can't be replayed against another club. This is a convenience, not
// security: anyone can pick any player on the whoami page.
type sessions struct{ secret []byte }

func cookieName(slug string) string { return "mc_" + slug }

func (s sessions) mac(slug string, playerID int64) string {
	h := hmac.New(sha256.New, s.secret)
	h.Write([]byte(slug + ":" + strconv.FormatInt(playerID, 10)))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

func (s sessions) sign(slug string, playerID int64) string {
	return strconv.FormatInt(playerID, 10) + "." + s.mac(slug, playerID)
}

// verify returns the player ID in value if the signature is valid for slug.
func (s sessions) verify(slug, value string) (int64, bool) {
	idStr, mac, ok := strings.Cut(value, ".")
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return 0, false
	}
	if !hmac.Equal([]byte(mac), []byte(s.mac(slug, id))) {
		return 0, false
	}
	return id, true
}
