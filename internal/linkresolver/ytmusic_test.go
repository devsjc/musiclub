package linkresolver

import (
	"compress/gzip"
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/prettyirrelevant/ytmusicapi"
	"github.com/stretchr/testify/require"
)

func TestYTMusicIntegration(t *testing.T) {
	// 1. Unconfigured provider fails cleanly
	require.Error(t, SetupYTMusic("not json"))
	_, err := NewYTMusicProvider().Search(context.Background(), "Queen", "Radio Ga Ga")
	require.Error(t, err, "Search without Setup should fail before the network")

	// 2. Setup a test server that expects the auth header and returns a gzipped song result
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/youtubei/v1/search", r.URL.Path)
		require.Equal(t, "https://music.youtube.com", r.Header.Get("Origin"))
		require.Contains(t, r.Header.Get("Authorization"), "SAPISIDHASH")
		require.True(t, strings.Contains(r.Header.Get("Cookie"), "VISITOR=a;__Secure-3PAPISID=secret;OTHER=b"))

		// Return a realistic song search result, gzipped
		response := `{
			"contents": {
				"tabbedSearchResultsRenderer": {
					"tabs": [{
						"tabRenderer": {
							"content": {
								"sectionListRenderer": {
									"contents": [{
										"musicShelfRenderer": {
											"title": {"runs": [{"text": "Songs"}]},
											"contents": [{
												"musicResponsiveListItemRenderer": {
													"flexColumns": [
														{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "Radio Ga Ga"}]}}},
														{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "Queen"}]}}}
													],
													"overlay": {
														"musicItemThumbnailOverlayRenderer": {
															"content": {
																"musicPlayButtonRenderer": {
																	"playNavigationEndpoint": {
																		"watchEndpoint": {"videoId": "vid123"}
																	}
																}
															}
														}
													}
												}
											}]
										}
									}]
								}
							}
						}
					}]
				}
			}
		}`

		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			w.Header().Set("Content-Encoding", "gzip")
			gz := gzip.NewWriter(w)
			_, _ = gz.Write([]byte(response))
			gz.Close()
		} else {
			_, _ = w.Write([]byte(response))
		}
	}))
	defer ts.Close()

	// 3. Hijack http.DefaultTransport to route all traffic to the test server
	oldTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = oldTransport })

	testTransport := oldTransport.(*http.Transport).Clone()
	testTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	testTransport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		// Route anything matching the API host to our test server
		if strings.HasPrefix(addr, "music.youtube.com:") {
			return net.Dial("tcp", ts.Listener.Addr().String())
		}
		return net.Dial(network, addr)
	}
	http.DefaultTransport = testTransport

	// 4. Setup ytmusicapi with headers that have spaces and dangerous accept-encoding
	headers := `{
		"cookie": "VISITOR=a; __Secure-3PAPISID=secret; OTHER=b",
		"origin": "https://music.youtube.com",
		"accept-encoding": "gzip, deflate",
		"content-encoding": "gzip"
	}`
	require.NoError(t, SetupYTMusic(headers))
	t.Cleanup(func() { os.Unsetenv("YTMUSIC_HEADERS_JSON") })

	// 5. Search should work, proving go handled gzip decompression
	matches, err := NewYTMusicProvider().Search(context.Background(), "Queen", "Radio")
	require.NoError(t, err)
	require.Len(t, matches, 1)
	require.Equal(t, "vid123", matches[0].ExternalID)
	require.Equal(t, "Queen", matches[0].Artist)
}

func TestToMatchesKeepsPlayableSongs(t *testing.T) {
	got := toMatches([]ytmusicapi.SearchResult{
		{ResultType: "song", VideoID: "abc_-123", Title: "Radio Ga Ga", Artistes: []string{"Queen"}},
		{ResultType: "song", VideoID: "def", Title: "Under Pressure", Artistes: []string{"Queen", "David Bowie"}},
		{ResultType: "video", VideoID: "vid", Title: "A music video", Artistes: []string{"Queen"}},
		{ResultType: "song", VideoID: "", Title: "Unplayable", Artistes: []string{"Queen"}},
		{ResultType: "artist", Artistes: []string{"Queen"}},
	})
	require.Equal(t, []Match{
		{ExternalID: "abc_-123", URL: MatchURLPrefix + "abc_-123", Title: "Radio Ga Ga", Artist: "Queen"},
		{ExternalID: "def", URL: MatchURLPrefix + "def", Title: "Under Pressure", Artist: "Queen, David Bowie"},
	}, got)
	require.Empty(t, toMatches(nil))
}
