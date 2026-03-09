package extractors

import (
	"net/url"
	"strings"

	"github.com/kashfshah/memory-palace/store"
)

// BlockedDomains contains domains whose records should be deleted entirely.
// Matched against the hostname portion of the URL (exact or suffix match).
// Organised by category for maintainability.
var BlockedDomains = []string{
	// --- Adult video / tube sites ---
	"pornhub.com",
	"xhamster.com",
	"xnxx.com",
	"xvideos.com",
	"youporn.com",
	"redtube.com",
	"tube8.com",
	"spankbang.com",
	"eporner.com",
	"txxx.com",
	"tnaflix.com",
	"beeg.com",
	"drtuber.com",
	"hdtube.porn",
	"4tube.com",
	"empflix.com",
	"gotporn.com",
	"hardsextube.com",
	"sunporno.com",
	"hclips.com",
	"xtube.com",
	"fuq.com",
	"porn.com",
	"porntrex.com",
	"fapster.xxx",
	"sexvid.xxx",
	"pornone.com",
	"keezmovies.com",
	"porndig.com",
	"videosection.com",
	"vjav.com",
	"javhd.net",
	"caribbeancom.com",
	"hey.xxx",

	// --- Live cam sites ---
	"chaturbate.com",
	"cam4.com",
	"myfreecams.com",
	"livejasmin.com",
	"streamate.com",
	"bongacams.com",
	"stripchat.com",
	"camsoda.com",
	"flirt4free.com",
	"jerkmate.com",
	"imlive.com",
	"xcams.com",
	"sexier.com",

	// --- Adult creator / paywall platforms ---
	"onlyfans.com",
	"fansly.com",
	"manyvids.com",
	"clips4sale.com",
	"niteflirt.com",
	"loyalfans.com",
	"fancentro.com",
	"admire.me",
	"erome.com",
	"fapello.com",
	"thothub.to",
	"coomer.party",
	"kemono.party",

	// --- Adult image boards / galleries ---
	"imagefap.com",
	"motherless.com",
	"xnxx.com",
	"theync.com",
	"hentai-foundry.com",
	"e-hentai.org",
	"nhentai.net",
	"hentai2read.com",
	"rule34.xxx",
	"rule34.paheal.net",
	"gelbooru.com",
	"danbooru.donmai.us",
	"sankakucomplex.com",
	"konachan.com",
	"yande.re",
	"luscious.net",
	"sexyfur.com",
	"pornpics.com",
	"bravoteens.com",

	// --- Adult dating / hookup / escort ---
	"adultfriendfinder.com",
	"ashleymadison.com",
	"seeking.com",
	"alt.com",
	"fetlife.com",
	"swinglifestyle.com",
	"kasidie.com",
	"pof.com", // match carefully — primarily dating but high NSFW signal
	"fling.com",
	"xmeets.com",
	"skipthegames.com",
	"eros.com",
	"slixa.com",
	"tryst.link",
	"listcrawler.com",
	"bedpage.com",
	"cityxguide.com",
	"adultsearch.com",
	"megapersonals.eu",

	// --- Shock / gore / extreme content ---
	"bestgore.com",
	"theync.com",
	"goregrish.com",
	"efukt.com",
	"rotten.com",
	"liveleak.com",
	"kaotic.com",
	"nsfw.xxx",
	"watchpeopledie.tv",
	"documenting-reality.com",

	// --- Self-harm / suicide facilitation ---
	"sanctioned-suicide.net",
	"sanctionedsuicide.net",
	"sanctionedsuicide.com",

	// --- Gay / LGBTQ+ adult (NSFW-specific sites, not general pride/community) ---
	"grindr.com",
	"web.grindr.com",
	"scruff.com",
	"gaymaletube.com",
	"gaytube.com",
	"xtube.com",
	"boyfriendtv.com",
	"men.com",
	"thegay.com",

	// --- Other known NSFW / bad-actor infra ---
	"element.envs.net",
	"thisvid.com",
	"pervertslut.com",
	"pornrox.com",
	"sexu.com",
	"eroprofile.com",
}

// BlockedTitleSubstrings causes a record to be deleted when found in the title
// (case-insensitive). Targets content that slips through unknown domains.
var BlockedTitleSubstrings = []string{
	// CSAM — zero tolerance
	"child porn",
	"cp porn",
	"loli porn",
	"shota porn",
	"underage porn",
	"jailbait",

	// Extreme violence / gore
	"cat torture",
	"animal torture",
	"snuff film",
	"execution video",
	"beheading video",
	"murder video",
	"gore video",

	// Self-harm facilitation
	"how to kill yourself",
	"suicide method",
	"painless suicide",
	"suicide note template",
}

// KagiTranslateHost matches Kagi Translate URLs — these get stripped to bare domain.
const KagiTranslateHost = "translate.kagi.com"

// AuthParams are URL query parameter names that carry credentials or tokens.
// These get stripped from URLs while preserving the rest of the URL.
var AuthParams = []string{
	"token",
	"access_token",
	"auth_token",
	"resetToken",
	"reset_token",
	"session",
	"session_id",
	"sessionid",
	"code",
	"auth",
	"jwt",
	"api_key",
	"apikey",
	"key",
	"secret",
	"password",
	"passwd",
	"confirmation_token",
	"verify_token",
	"id_token",
	"refresh_token",
	"expires_in",
	"token_type",
	"state",
	"dsh",
	"otg",
	"dvctoken",
}

// IsBlockedDomain returns true if the URL belongs to a blocked domain,
// or if a blocked domain appears anywhere in the URL (including URL-encoded
// in query params, e.g. OAuth redirects to blocked sites).
func IsBlockedDomain(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, d := range BlockedDomains {
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	// Also check the decoded full URL for blocked domain references
	// (catches OAuth redirects with url-encoded blocked domains)
	decoded, _ := url.QueryUnescape(rawURL)
	lower := strings.ToLower(decoded)
	for _, d := range BlockedDomains {
		if strings.Contains(lower, d) {
			return true
		}
	}
	return false
}

// HasBlockedTitle returns true if the title contains a blocked substring.
func HasBlockedTitle(title string) bool {
	lower := strings.ToLower(title)
	for _, sub := range BlockedTitleSubstrings {
		if strings.Contains(lower, sub) {
			return true
		}
	}
	return false
}

// IsKagiTranslate returns true if the URL belongs to Kagi Translate,
// including URLs with translate.kagi.com encoded in query params.
func IsKagiTranslate(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if strings.ToLower(u.Hostname()) == KagiTranslateHost {
		return true
	}
	decoded, _ := url.QueryUnescape(rawURL)
	return strings.Contains(strings.ToLower(decoded), KagiTranslateHost)
}

// StripAuthParams removes credential-bearing query and fragment parameters from a URL.
// Handles both ?token=... (query) and #access_token=... (fragment) patterns.
func StripAuthParams(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	changed := false

	// Strip from query string
	q := u.Query()
	for _, param := range AuthParams {
		if q.Has(param) {
			q.Del(param)
			changed = true
		}
	}
	if changed {
		u.RawQuery = q.Encode()
	}

	// Strip from fragment (handles #access_token=eyJ... patterns)
	if u.Fragment != "" {
		frag, err := url.ParseQuery(u.Fragment)
		if err == nil {
			fragChanged := false
			for _, param := range AuthParams {
				if frag.Has(param) {
					frag.Del(param)
					fragChanged = true
				}
			}
			if fragChanged {
				u.Fragment = frag.Encode()
				changed = true
			}
		}
	}

	if !changed {
		return rawURL
	}
	return u.String()
}

// SanitizeRecords filters and cleans a slice of records:
// - Deletes records from blocked domains
// - Deletes records with blocked title substrings
// - Replaces Kagi Translate URLs with bare "https://translate.kagi.com"
// - Strips auth params from all remaining URLs
func SanitizeRecords(records []store.Record) []store.Record {
	out := make([]store.Record, 0, len(records))
	for _, r := range records {
		if IsBlockedDomain(r.URL) {
			continue
		}
		if HasBlockedTitle(r.Title) {
			continue
		}
		if IsKagiTranslate(r.URL) {
			r.URL = "https://translate.kagi.com"
			r.Title = "Kagi Translate"
			r.Body = ""
		}
		r.URL = StripAuthParams(r.URL)
		out = append(out, r)
	}
	return out
}
