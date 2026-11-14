package handlers

import (
	"context"
	"database/sql"
	"errors"
	"html/template"
	"net/http"
	"regexp"

	"ispilolite/api/dto"
	"ispilolite/internal/models"
	"ispilolite/internal/repository/postgres"
	"ispilolite/pkg/profilelink"
)

type publicProfileReader interface {
	BySlug(context.Context, string) (*models.PublicProfile, error)
	ByUserID(context.Context, string) (*models.PublicProfile, error)
}

type PublicProfileHandler struct {
	repo  publicProfileReader
	links *profilelink.Builder
}

func NewPublicProfileHandler(links *profilelink.Builder) (*PublicProfileHandler, error) {
	repo, err := postgres.NewProfileRepository()
	if err != nil {
		return nil, err
	}
	return &PublicProfileHandler{repo: repo, links: links}, nil
}

var publicSlugRE = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*-[0-9]{4}-[0-9a-f]{32}$`)

func (h *PublicProfileHandler) load(ctx context.Context, slug string) (*models.PublicProfile, error) {
	if len(slug) > 96 || !publicSlugRE.MatchString(slug) {
		return nil, sql.ErrNoRows
	}
	profile, err := h.repo.BySlug(ctx, slug)
	if err == nil {
		profile.ProfileURL = h.links.URL(profile.ProfilePath)
	}
	return profile, err
}

// JSON resolves the same stable link for Flutter and other API clients.
func (h *PublicProfileHandler) JSON(w http.ResponseWriter, r *http.Request) {
	profile, err := h.load(r.Context(), pathParam(r.URL.Path, "/api/v1/profiles/"))
	if errors.Is(err, sql.ErrNoRows) {
		respondWithErrorRaw(w, http.StatusNotFound, "profile not found")
		return
	}
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: profile})
}

func (h *PublicProfileHandler) Mine(w http.ResponseWriter, r *http.Request) {
	profile, err := h.repo.ByUserID(r.Context(), userIDFromContext(r.Context()))
	if errors.Is(err, sql.ErrNoRows) {
		respondWithErrorRaw(w, http.StatusNotFound, "profile not found")
		return
	}
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	profile.ProfileURL = h.links.URL(profile.ProfilePath)
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: profile})
}

var publicProfilePage = template.Must(template.New("profile").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="description" content="View {{.Name}} on Ispilo Lite">
<meta property="og:title" content="{{.Name}} — Ispilo Lite">
<meta property="og:description" content="{{.Role}} profile on Ispilo Lite">
<link rel="canonical" href="{{.ProfileURL}}"><title>{{.Name}} — Ispilo Lite</title>
<style>body{font:16px/1.5 system-ui,sans-serif;background:#f5f7fb;color:#182438;margin:0;padding:2rem}
main{max-width:520px;margin:7vh auto;background:white;padding:2rem;border-radius:1rem;box-shadow:0 8px 30px #18243812}
img{width:88px;height:88px;border-radius:50%;object-fit:cover}h1{margin:.5rem 0}.role{text-transform:capitalize;color:#54657c}
.stats{display:flex;gap:2rem;margin-top:1.5rem}.stats strong{display:block;font-size:1.25rem}</style>
</head><body><main>{{if .AvatarURL}}<img src="{{.AvatarURL}}" alt="{{.Name}} profile photo">{{end}}
<h1>{{.Name}}</h1><p class="role">{{.Role}}</p>
<div class="stats"><div><strong>{{.FollowerCount}}</strong> followers</div><div><strong>{{.FollowingCount}}</strong> following</div></div>
</main></body></html>`))

// Web makes a shared /u/... link directly viewable in a browser even without
// a Flutter web build. It uses the same safe public fields as the JSON API.
func (h *PublicProfileHandler) Web(w http.ResponseWriter, r *http.Request) {
	profile, err := h.load(r.Context(), pathParam(r.URL.Path, "/u/"))
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "profile unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Robots-Tag", "index, follow")
	if err := publicProfilePage.Execute(w, profile); err != nil {
		return
	}
}
