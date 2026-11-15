package handlers

import (
	"context"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"ispilolite/api/dto"
	"ispilolite/internal/models"
	"ispilolite/internal/repository/postgres"
	"ispilolite/pkg/profilelink"
)

type publicCoverageReader interface {
	Coverage(context.Context, string, string, string) (*models.PublicCoveragePage, error)
	RecordView(context.Context, string, string, string) error
}

// PublicDiscoveryHandler serves server-rendered area pages and the matching
// JSON endpoint. Pages are public and contain no customer identities.
type PublicDiscoveryHandler struct {
	repo  publicCoverageReader
	links *profilelink.Builder
}

func NewPublicDiscoveryHandler(links *profilelink.Builder) (*PublicDiscoveryHandler, error) {
	repo, err := postgres.NewDiscoveryRepository()
	if err != nil {
		return nil, err
	}
	return newPublicDiscoveryHandler(repo, links), nil
}

func newPublicDiscoveryHandler(repo publicCoverageReader, links *profilelink.Builder) *PublicDiscoveryHandler {
	return &PublicDiscoveryHandler{repo: repo, links: links}
}

// Web renders an SEO-friendly coverage page for /places/:county,
// /places/:county/:town or /places/:county/:town/:estate.
func (h *PublicDiscoveryHandler) Web(w http.ResponseWriter, r *http.Request) {
	county, town, estate, ok := parsePlacePath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	page, err := h.load(r.Context(), county, town, estate)
	if err != nil {
		log.Printf("public coverage page failed: %v", err)
		http.Error(w, "coverage page unavailable", http.StatusInternalServerError)
		return
	}
	h.recordView(r, page)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Robots-Tag", "index, follow")
	w.Header().Set("Cache-Control", "public, max-age=300")
	if err := publicCoveragePage.Execute(w, page); err != nil {
		log.Printf("public coverage template failed: %v", err)
	}
}

// JSON returns the same public data for Flutter, partner sites and search
// indexes that prefer structured responses.
func (h *PublicDiscoveryHandler) JSON(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	county := strings.TrimSpace(query.Get("county"))
	town := strings.TrimSpace(query.Get("town"))
	estate := strings.TrimSpace(query.Get("estate"))
	if !validPlaceSegment(county) || (town != "" && !validPlaceSegment(town)) || (estate != "" && !validPlaceSegment(estate)) || (estate != "" && town == "") {
		respondWithErrorRaw(w, http.StatusBadRequest, "county is required; town is required when estate is provided; place names must be valid")
		return
	}
	page, err := h.load(r.Context(), county, town, estate)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: page})
}

func (h *PublicDiscoveryHandler) load(ctx context.Context, county, town, estate string) (*models.PublicCoveragePage, error) {
	page, err := h.repo.Coverage(ctx, county, town, estate)
	if err != nil {
		return nil, err
	}
	if page == nil {
		page = &models.PublicCoveragePage{}
	}
	page.ISPCount = len(page.ISPs)
	if strings.TrimSpace(page.County) == "" {
		page.County = displayPlace(county)
	} else {
		page.County = strings.TrimSpace(page.County)
	}
	page.Town = displayPlace(town)
	page.Estate = displayPlace(estate)
	page.PageType = coveragePageType(page.Town, page.Estate)
	page.CanonicalURL = h.canonicalURL(page)
	page.Title = "Internet providers in " + areaLabel(page) + " | Ispilo Lite"
	page.Description = coverageDescription(page)
	if len(page.ISPs) > 0 {
		page.ImageURL = safeImageURL(page.ISPs[0].LogoURL)
		if h.links != nil {
			for _, isp := range page.ISPs {
				isp.ProfileURL = h.links.URL(isp.ProfilePath)
			}
		}
	}
	return page, nil
}

func (h *PublicDiscoveryHandler) canonicalURL(page *models.PublicCoveragePage) string {
	segments := []string{placeSlug(page.County)}
	if page.Town != "" {
		segments = append(segments, placeSlug(page.Town))
	}
	if page.Estate != "" {
		segments = append(segments, placeSlug(page.Estate))
	}
	origin := ""
	if h.links != nil {
		origin = strings.TrimRight(h.links.Origin(), "/")
	}
	return origin + "/places/" + strings.Join(segments, "/")
}

func (h *PublicDiscoveryHandler) recordView(r *http.Request, page *models.PublicCoveragePage) {
	if h.repo == nil || page.CanonicalURL == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	key := strings.ToLower(strings.Join([]string{page.County, page.Town, page.Estate}, "|"))
	if err := h.repo.RecordView(ctx, page.PageType, key, referrerHost(r.Referer())); err != nil {
		log.Printf("public coverage view tracking failed: %v", err)
	}
}

func coveragePageType(town, estate string) string {
	if estate != "" {
		return "estate"
	}
	if town != "" {
		return "town"
	}
	return "county"
}

func areaLabel(page *models.PublicCoveragePage) string {
	parts := make([]string, 0, 3)
	if page.Estate != "" {
		parts = append(parts, page.Estate)
	}
	if page.Town != "" {
		parts = append(parts, page.Town)
	}
	if page.County != "" {
		parts = append(parts, page.County)
	}
	if len(parts) == 0 {
		return "Kenya"
	}
	return strings.Join(parts, ", ")
}

func coverageDescription(page *models.PublicCoveragePage) string {
	area := areaLabel(page)
	if page.ISPCount == 0 {
		return "No internet providers are listed in " + area + " yet. Check coverage and join the waitlist on Ispilo Lite."
	}
	providerWord := "providers"
	if page.ISPCount == 1 {
		providerWord = "provider"
	}
	packageWord := "packages"
	if page.PackageCount == 1 {
		packageWord = "package"
	}
	return fmt.Sprintf("Compare %d internet %s and %d %s in %s. See prices, speeds, ratings and coverage on Ispilo Lite.", page.ISPCount, providerWord, page.PackageCount, packageWord, area)
}

func parsePlacePath(path string) (county, town, estate string, ok bool) {
	if !strings.HasPrefix(path, "/places/") {
		return "", "", "", false
	}
	remainder := strings.Trim(strings.TrimPrefix(path, "/places/"), "/")
	if remainder == "" {
		return "", "", "", false
	}
	parts := strings.Split(remainder, "/")
	if len(parts) > 3 {
		return "", "", "", false
	}
	for _, part := range parts {
		if !validPlaceSegment(part) {
			return "", "", "", false
		}
	}
	county = strings.TrimSpace(parts[0])
	if len(parts) > 1 {
		town = strings.TrimSpace(parts[1])
	}
	if len(parts) > 2 {
		estate = strings.TrimSpace(parts[2])
	}
	return county, town, estate, true
}

func validPlaceSegment(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 80 {
		return false
	}
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		switch r {
		case ' ', '-', '\'', '.', '_', ',':
			continue
		default:
			return false
		}
	}
	return true
}

// displayPlace turns a URL slug into a readable label. Database values are
// preferred when available; this is the fallback for empty result sets.
func displayPlace(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "-", " "))
	if value == "" {
		return ""
	}
	words := strings.Fields(value)
	for i, word := range words {
		runes := []rune(strings.ToLower(word))
		if len(runes) == 0 {
			continue
		}
		runes[0] = unicode.ToUpper(runes[0])
		words[i] = string(runes)
	}
	return strings.Join(words, " ")
}

func placeSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastHyphen := false
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastHyphen = false
			continue
		}
		if b.Len() > 0 && !lastHyphen {
			b.WriteByte('-')
			lastHyphen = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "area"
	}
	return slug
}

func safeImageURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return raw
}

func referrerHost(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

var publicCoveragePage = template.Must(template.New("coverage").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<meta name="description" content="{{.Description}}">
<meta name="robots" content="index, follow">
<link rel="canonical" href="{{.CanonicalURL}}">
<meta property="og:type" content="website">
<meta property="og:site_name" content="Ispilo Lite">
<meta property="og:title" content="{{.Title}}">
<meta property="og:description" content="{{.Description}}">
<meta property="og:url" content="{{.CanonicalURL}}">
{{if .ImageURL}}<meta property="og:image" content="{{.ImageURL}}">{{end}}
<meta name="twitter:card" content="summary_large_image">
<meta name="twitter:title" content="{{.Title}}">
<meta name="twitter:description" content="{{.Description}}">
<style>
:root{color-scheme:light}*{box-sizing:border-box}body{margin:0;font:16px/1.55 system-ui,-apple-system,Segoe UI,sans-serif;background:#f4f7fb;color:#17233a}
header{background:#132a4f;color:#fff}header .wrap{max-width:1040px;margin:auto;padding:1rem 1.25rem;display:flex;align-items:center;justify-content:space-between;gap:1rem}
a{color:#0b62d6}header a{color:#fff;text-decoration:none;font-weight:700;font-size:1.15rem}
main{max-width:1040px;margin:auto;padding:2rem 1.25rem 4rem}h1{font-size:clamp(1.8rem,4vw,2.8rem);line-height:1.15;margin:.25rem 0 1rem}
.lede{font-size:1.1rem;color:#42536e;max-width:760px}.share{display:flex;flex-wrap:wrap;gap:.5rem;margin:1.25rem 0 2rem}
.share a{text-decoration:none;border:1px solid #c9d6e8;background:#fff;color:#17345f;padding:.5rem .8rem;border-radius:999px;font-weight:600;font-size:.9rem}
.grid{display:grid;gap:1.25rem}.card{background:#fff;border:1px solid #dce5f2;border-radius:1rem;padding:1.25rem;box-shadow:0 8px 24px #18345f0d}
.head{display:flex;gap:1rem;align-items:flex-start}.logo{width:64px;height:64px;border-radius:14px;object-fit:cover;background:#edf2f9;flex:none}
h2{margin:0;font-size:1.35rem}.meta{color:#5b6b84;margin:.25rem 0}.badge{display:inline-block;background:#e7f6ec;color:#176b37;border-radius:999px;padding:.15rem .55rem;font-size:.78rem;font-weight:700}
.badge.likely{background:#fff5dc;color:#7a5200}.badge.survey{background:#eef2ff;color:#34459a}.badge.no{background:#fdecec;color:#8b2020}
.price{font-weight:700;color:#17345f}.table-wrap{overflow-x:auto;margin-top:1rem}table{width:100%;border-collapse:collapse;font-size:.93rem}
th,td{text-align:left;padding:.6rem .5rem;border-bottom:1px solid #edf1f7}th{color:#5b6b84;font-size:.78rem;text-transform:uppercase;letter-spacing:.04em}
.actions{display:flex;flex-wrap:wrap;gap:.6rem;margin-top:1rem}.button{display:inline-block;background:#0b62d6;color:#fff;text-decoration:none;padding:.6rem 1rem;border-radius:.65rem;font-weight:700}
.note{background:#fff;border-left:4px solid #0b62d6;border-radius:.5rem;padding:1rem 1.1rem;margin-top:2rem;color:#42536e;font-size:.94rem}
.empty{background:#fff;border:1px dashed #b9c9df;border-radius:1rem;padding:1.5rem}.empty h2{margin-bottom:.5rem}
footer{max-width:1040px;margin:auto;padding:0 1.25rem 3rem;color:#6b7a91;font-size:.9rem}
@media(min-width:760px){.grid{grid-template-columns:repeat(2,minmax(0,1fr))}}
</style></head><body>
<header><div class="wrap"><a href="/">Ispilo Lite</a><span>Internet, simplified</span></div></header>
<main>
<p class="meta">{{.PageType}} coverage</p>
<h1>Internet providers in {{.County}}{{if .Town}}, {{.Town}}{{end}}{{if .Estate}}, {{.Estate}}{{end}}</h1>
<p class="lede">{{.Description}}</p>
<div class="share"><a target="_blank" rel="noopener" href="https://wa.me/?text={{urlquery .Title}}%20{{urlquery .CanonicalURL}}">Share on WhatsApp</a><a target="_blank" rel="noopener" href="https://www.facebook.com/sharer/sharer.php?u={{urlquery .CanonicalURL}}">Share on Facebook</a><a target="_blank" rel="noopener" href="https://twitter.com/intent/tweet?text={{urlquery .Title}}&amp;url={{urlquery .CanonicalURL}}">Share on X</a></div>
{{if .ISPs}}<section class="grid" aria-label="Internet providers">
{{range .ISPs}}<article class="card">
<div class="head">{{if .LogoURL}}<img class="logo" src="{{.LogoURL}}" alt="{{.Name}} logo" loading="lazy">{{end}}<div><h2>{{.Name}}</h2><p class="meta">{{if gt .ReviewCount 0}}★ {{printf "%.1f" .Rating}} from {{.ReviewCount}} review{{if ne .ReviewCount 1}}s{{end}}{{else}}New on Ispilo Lite{{end}} · {{.TechniciansAvailable}} technician{{if ne .TechniciansAvailable 1}}s{{end}} available</p>
{{if eq .Serviceability "serviceable"}}<span class="badge">Serviceable in this area</span>{{else if eq .Serviceability "likely_serviceable"}}<span class="badge likely">Likely serviceable</span>{{else if eq .Serviceability "survey_required"}}<span class="badge survey">Survey required</span>{{else if eq .Serviceability "not_serviceable"}}<span class="badge no">Not currently serviceable</span>{{end}}</div></div>
{{if .Description}}<p>{{.Description}}</p>{{end}}
{{if gt .FromPrice 0.0}}<p class="price">Packages from KES {{printf "%.0f" .FromPrice}}</p>{{end}}
{{if .Packages}}<div class="table-wrap"><table><thead><tr><th>Package</th><th>Speed</th><th>Price</th><th>Billing</th></tr></thead><tbody>
{{range .Packages}}<tr><td>{{.Name}}</td><td>{{if .Speed}}{{.Speed}}{{else}}{{printf "%.0f" .SpeedValue}} {{.SpeedUnit}}{{end}}</td><td>{{if gt .Price 0.0}}KES {{printf "%.0f" .Price}}{{else}}Contact ISP{{end}}</td><td>{{.BillingCycle}}</td></tr>{{end}}
</tbody></table></div>{{else}}<p class="meta">No active public packages listed yet.</p>{{end}}
{{if .ProfileURL}}<div class="actions"><a class="button" href="{{.ProfileURL}}">View {{.Name}} profile</a></div>{{end}}
</article>{{end}}
</section>{{else}}<section class="empty"><h2>No providers listed here yet</h2><p>Coverage is added by ISPs and verified as they expand. Check back soon, or join the waitlist for this area in the Ispilo Lite app.</p></section>{{end}}
<div class="note"><strong>Before you pay:</strong> coverage shown here is published by each ISP. Confirm your exact building or estate during the quotation step. Prices can vary by county, package and installation address.</div>
</main>
<footer>Ispilo Lite helps customers compare internet providers, packages and installation options in Kenya.</footer>
</body></html>`))
