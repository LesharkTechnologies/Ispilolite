package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func mediaRequest(t *testing.T, body, userID, role string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/media/upload-signature", strings.NewReader(body))
	ctx := context.Background()
	if userID != "" {
		ctx = context.WithValue(ctx, "userID", userID)
	}
	if role != "" {
		ctx = context.WithValue(ctx, "userRole", role)
	}
	return req.WithContext(ctx)
}

func TestMediaSignatureReturnsSignedCloudinaryParams(t *testing.T) {
	handler := newMediaHandler("demo-cloud", "public-key", "secret", "ispilolite")
	handler.now = func() time.Time { return time.Unix(1700000000, 0) }
	req := mediaRequest(t, `{"kind":"installation_proof","file_size":2048,"content_type":"image/jpeg"}`, "user-123", "technician")
	rec := httptest.NewRecorder()

	handler.Signature(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Kind           string            `json:"kind"`
			CloudName      string            `json:"cloud_name"`
			UploadURL      string            `json:"upload_url"`
			Folder         string            `json:"folder"`
			Timestamp      int64             `json:"timestamp"`
			ExpiresAt      int64             `json:"expires_at"`
			MaxFileSize    int64             `json:"max_file_size"`
			AllowedFormats []string          `json:"allowed_formats"`
			UploadParams   map[string]string `json:"upload_params"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if !response.Success {
		t.Fatal("success = false")
	}
	if response.Data.Kind != "installation_proof" || response.Data.CloudName != "demo-cloud" {
		t.Fatalf("unexpected data: %+v", response.Data)
	}
	if response.Data.Folder != "ispilolite/installation-proofs/user-123" {
		t.Fatalf("folder = %q", response.Data.Folder)
	}
	if response.Data.UploadURL != "https://api.cloudinary.com/v1_1/demo-cloud/image/upload" {
		t.Fatalf("upload_url = %q", response.Data.UploadURL)
	}
	if response.Data.Timestamp != 1700000000 || response.Data.ExpiresAt != 1700000600 {
		t.Fatalf("timestamps = %d %d", response.Data.Timestamp, response.Data.ExpiresAt)
	}
	if response.Data.MaxFileSize != mediaMaxProof {
		t.Fatalf("max_file_size = %d", response.Data.MaxFileSize)
	}
	if got := response.Data.UploadParams["signature"]; got != "91bc9413ef03bf627ced6bd2cf544b94a759a574" {
		t.Fatalf("signature = %q", got)
	}
	if response.Data.UploadParams["api_key"] != "public-key" || response.Data.UploadParams["folder"] != response.Data.Folder {
		t.Fatalf("unexpected upload params: %+v", response.Data.UploadParams)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Fatal("response leaked the Cloudinary API secret")
	}
}

func TestMediaSignatureRequiresConfigurationAndAuth(t *testing.T) {
	unconfigured := newMediaHandler("", "", "", "")
	rec := httptest.NewRecorder()
	unconfigured.Signature(rec, mediaRequest(t, `{"kind":"profile"}`, "user-1", "customer"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured status = %d", rec.Code)
	}

	handler := newMediaHandler("demo-cloud", "public-key", "secret", "")
	rec = httptest.NewRecorder()
	handler.Signature(rec, mediaRequest(t, `{"kind":"profile"}`, "", ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", rec.Code)
	}
}

func TestMediaSignatureEnforcesKindRoles(t *testing.T) {
	handler := newMediaHandler("demo-cloud", "public-key", "secret", "")
	tests := []struct {
		kind   string
		role   string
		status int
	}{
		{"installation_proof", "technician", http.StatusOK},
		{"installation_proof", "customer", http.StatusForbidden},
		{"promotion", "isp", http.StatusOK},
		{"promotion", "technician", http.StatusForbidden},
		{"profile", "customer", http.StatusOK},
		{"review", "admin", http.StatusOK},
		{"chat", "isp", http.StatusOK},
	}
	for _, test := range tests {
		rec := httptest.NewRecorder()
		handler.Signature(rec, mediaRequest(t, `{"kind":"`+test.kind+`"}`, "user-1", test.role))
		if rec.Code != test.status {
			t.Fatalf("%s as %s = %d, want %d: %s", test.kind, test.role, rec.Code, test.status, rec.Body.String())
		}
	}
}

func TestMediaSignatureRejectsInvalidRequests(t *testing.T) {
	handler := newMediaHandler("demo-cloud", "public-key", "secret", "")
	tests := []struct {
		name   string
		body   string
		role   string
		status int
	}{
		{"unknown kind", `{"kind":"executable"}`, "technician", http.StatusBadRequest},
		{"negative size", `{"kind":"profile","file_size":-1}`, "customer", http.StatusBadRequest},
		{"too large", `{"kind":"installation_proof","file_size":16777216}`, "technician", http.StatusRequestEntityTooLarge},
		{"unsupported type", `{"kind":"profile","content_type":"application/pdf"}`, "customer", http.StatusBadRequest},
		{"invalid json", `{"kind":`, "customer", http.StatusBadRequest},
	}
	for _, test := range tests {
		rec := httptest.NewRecorder()
		handler.Signature(rec, mediaRequest(t, test.body, "user-1", test.role))
		if rec.Code != test.status {
			t.Fatalf("%s = %d, want %d: %s", test.name, rec.Code, test.status, rec.Body.String())
		}
	}
}

func TestMediaSignatureChangesWithTimestamp(t *testing.T) {
	handler := newMediaHandler("demo-cloud", "public-key", "secret", "")
	handler.now = func() time.Time { return time.Unix(1700000000, 0) }
	first := cloudinarySignature(map[string]string{"folder": "a", "timestamp": "1"}, "secret")
	second := cloudinarySignature(map[string]string{"folder": "a", "timestamp": "2"}, "secret")
	if first == second || len(first) != 40 {
		t.Fatalf("signatures = %q %q", first, second)
	}
}

func TestSanitizeFolderPath(t *testing.T) {
	if got := sanitizeFolderPath(""); got != "ispilolite" {
		t.Fatalf("empty = %q", got)
	}
	if got := sanitizeFolderPath(" my folder/prod! "); got != "my-folder/prod" {
		t.Fatalf("sanitized = %q", got)
	}
}

func TestMediaServerFallbackForwardsMultipartAndReturnsSecureURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasPrefix(r.URL.Path, "/v1_1/demo-cloud/image/upload") {
			t.Fatalf("unexpected Cloudinary request: %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("folder") != "ispilolite/profiles/user-1" {
			t.Fatalf("folder=%q", r.FormValue("folder"))
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		content, err := io.ReadAll(file)
		if err != nil {
			t.Fatal(err)
		}
		if string(content) != "\xff\xd8\xff\xe0image" {
			t.Fatalf("content=%q", content)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"secure_url":"https://res.cloudinary.com/demo/image/upload/v1/photo.jpg","public_id":"ispilolite/profiles/user-1/photo","format":"jpg","bytes":11,"width":100,"height":100,"resource_type":"image"}`))
	}))
	defer server.Close()
	h := newMediaHandler("demo-cloud", "public-key", "secret", "ispilolite")
	h.cloudinaryBaseURL = server.URL
	h.client = server.Client()

	var body bytes.Buffer
	multipartWriter := multipart.NewWriter(&body)
	_ = multipartWriter.WriteField("kind", "profile")
	part, err := multipartWriter.CreateFormFile("file", "photo.jpg")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("\xff\xd8\xff\xe0image"))
	if err := multipartWriter.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/media/upload", &body)
	req.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	ctx := context.WithValue(context.Background(), "userID", "user-1")
	ctx = context.WithValue(ctx, "userRole", "customer")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.Upload(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "https://res.cloudinary.com/demo/image/upload/v1/photo.jpg") {
		t.Fatalf("missing secure URL: %s", rec.Body.String())
	}
}
