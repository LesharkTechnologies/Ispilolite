package i18n

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

type localeKey struct{}

var catalogs = map[string]map[string]string{
	"en": {
		"common.internal_error": "Something went wrong. Please try again.",
		"common.unauthorized":   "Authentication is required.",
		"job.assigned":          "A provider has been assigned to your job.",
		"job.accepted":          "Your job has been accepted.",
		"job.in_progress":       "Work on your job is now in progress.",
		"job.completed":         "Your job has been completed.",
		"technician.arriving":   "Your technician is about five minutes away.",
		"chat.new_message":      "You have a new message about your job.",
		"escrow.held":           "Your payment is safely held until work is approved.",
	},
	"sw": {
		"common.internal_error": "Hitilafu imetokea. Tafadhali jaribu tena.",
		"common.unauthorized":   "Unahitaji kuingia kwenye akaunti.",
		"job.assigned":          "Mtoa huduma amepangiwa kazi yako.",
		"job.accepted":          "Kazi yako imekubaliwa.",
		"job.in_progress":       "Kazi yako imeanza kufanywa.",
		"job.completed":         "Kazi yako imekamilika.",
		"technician.arriving":   "Fundi wako yuko karibu kufika ndani ya dakika tano.",
		"chat.new_message":      "Una ujumbe mpya kuhusu kazi yako.",
		"escrow.held":           "Malipo yako yameshikiliwa salama hadi uidhinishe kazi.",
	},
}

func Normalize(locale string) string {
	locale = strings.ToLower(strings.TrimSpace(locale))
	if strings.HasPrefix(locale, "sw") {
		return "sw"
	}
	return "en"
}

func Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			locale := r.URL.Query().Get("lang")
			if locale == "" {
				locale = r.Header.Get("Accept-Language")
			}
			ctx := context.WithValue(r.Context(), localeKey{}, Normalize(locale))
			w.Header().Set("Content-Language", Locale(ctx))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func Locale(ctx context.Context) string {
	if value, ok := ctx.Value(localeKey{}).(string); ok {
		return value
	}
	return "en"
}
func T(ctx context.Context, key string, args ...interface{}) string {
	return Translate(Locale(ctx), key, args...)
}
func Translate(locale, key string, args ...interface{}) string {
	locale = Normalize(locale)
	value := catalogs[locale][key]
	if value == "" {
		value = catalogs["en"][key]
	}
	if value == "" {
		value = key
	}
	if len(args) > 0 {
		return fmt.Sprintf(value, args...)
	}
	return value
}
func Catalog(locale string) map[string]string {
	source := catalogs[Normalize(locale)]
	out := make(map[string]string, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}
