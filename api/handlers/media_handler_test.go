package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func testR2Handler() *MediaHandler {
	h := newMediaHandler(
		"https://account.r2.cloudflarestorage.com", "r2-access", "r2-secret",
		"ispilo-media", "https://media.ispilo.co.ke", "auto", "ispilolite",
	)
	h.now = func() time.Time { return time.Unix(1700000000, 0).UTC() }
	return h
}

func TestMediaSignatureReturnsR2PresignedPut(t *testing.T) {
	h := testR2Handler()
	req := mediaRequest(t, `{"kind":"installation_proof","file_name":"proof.jpg","file_size":2048,"content_type":"image/jpeg"}`, "user-123", "technician")
	rec := httptest.NewRecorder()
	h.Signature(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Kind        string            `json:"kind"`
			Method      string            `json:"method"`
			UploadURL   string            `json:"upload_url"`
			PublicURL   string            `json:"public_url"`
			ObjectKey   string            `json:"object_key"`
			ExpiresAt   int64             `json:"expires_at"`
			MaxFileSize int64             `json:"max_file_size"`
			Headers     map[string]string `json:"headers"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if !response.Success || response.Data.Method != http.MethodPut || response.Data.Kind != "installation_proof" {
		t.Fatalf("unexpected response: %+v", response)
	}
	if !strings.HasPrefix(response.Data.ObjectKey, "ispilolite/installation-proofs/user-123/2023/11/") || !strings.HasSuffix(response.Data.ObjectKey, ".jpg") {
		t.Fatalf("object_key=%q", response.Data.ObjectKey)
	}
	if response.Data.PublicURL != "https://media.ispilo.co.ke/"+response.Data.ObjectKey {
		t.Fatalf("public_url=%q", response.Data.PublicURL)
	}
	u, err := url.Parse(response.Data.UploadURL)
	if err != nil || u.Scheme != "https" || u.Host != "account.r2.cloudflarestorage.com" {
		t.Fatalf("upload_url=%q err=%v", response.Data.UploadURL, err)
	}
	if u.Query().Get("X-Amz-Algorithm") != "AWS4-HMAC-SHA256" || u.Query().Get("X-Amz-Signature") == "" || u.Query().Get("X-Amz-Expires") != "600" {
		t.Fatalf("presign query=%v", u.Query())
	}
	if strings.Contains(rec.Body.String(), "r2-secret") || strings.Contains(rec.Body.String(), "r2-access") {
		t.Fatal("response leaked R2 credentials")
	}
	if response.Data.ExpiresAt != 1700000600 || response.Data.MaxFileSize != mediaMaxProof || response.Data.Headers["Content-Type"] != "image/jpeg" {
		t.Fatalf("metadata=%+v", response.Data)
	}
}

func TestMediaSignatureRequiresR2ConfigurationAndAuth(t *testing.T) {
	unconfigured := newMediaHandler("", "", "", "", "", "", "")
	rec := httptest.NewRecorder()
	unconfigured.Signature(rec, mediaRequest(t, `{"kind":"profile","file_name":"a.jpg","content_type":"image/jpeg"}`, "user-1", "customer"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured status=%d", rec.Code)
	}
	h := testR2Handler()
	rec = httptest.NewRecorder()
	h.Signature(rec, mediaRequest(t, `{"kind":"profile","file_name":"a.jpg","content_type":"image/jpeg"}`, "", ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d", rec.Code)
	}
}

func TestMediaKindsEnforceRolesAndLimits(t *testing.T) {
	h := testR2Handler()
	tests := []struct {
		kind, role string
		status     int
	}{
		{"installation_proof", "technician", 200}, {"installation_proof", "customer", 403},
		{"job_request", "customer", 200}, {"billing_logo", "isp", 200},
		{"billing_logo", "customer", 403}, {"profile", "customer", 200},
		{"portfolio", "technician", 200}, {"document", "customer", 200},
	}
	for _, test := range tests {
		rec := httptest.NewRecorder()
		body := `{"kind":"` + test.kind + `","file_name":"a.jpg","file_size":100,"content_type":"image/jpeg"}`
		h.Signature(rec, mediaRequest(t, body, "user-1", test.role))
		if rec.Code != test.status {
			t.Fatalf("%s/%s=%d want=%d body=%s", test.kind, test.role, rec.Code, test.status, rec.Body.String())
		}
	}
}

func TestMediaSignatureRejectsInvalidInput(t *testing.T) {
	h := testR2Handler()
	tests := []struct {
		body, role string
		status     int
	}{
		{`{"kind":"executable","file_name":"a.jpg","content_type":"image/jpeg"}`, "technician", 400},
		{`{"kind":"profile","file_name":"a.jpg","file_size":-1,"content_type":"image/jpeg"}`, "customer", 413},
		{`{"kind":"installation_proof","file_name":"a.jpg","file_size":16777216,"content_type":"image/jpeg"}`, "technician", 413},
		{`{"kind":"profile","file_name":"a.pdf","content_type":"application/pdf"}`, "customer", 400},
		{`{"kind":`, "customer", 400},
	}
	for _, test := range tests {
		rec := httptest.NewRecorder()
		h.Signature(rec, mediaRequest(t, test.body, "user-1", test.role))
		if rec.Code != test.status {
			t.Fatalf("body=%s status=%d want=%d response=%s", test.body, rec.Code, test.status, rec.Body.String())
		}
	}
}

func TestMediaFallbackPutsBytesToR2(t *testing.T) {
	var uploaded []byte
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || !strings.HasPrefix(r.URL.Path, "/ispilo-media/ispilolite/profiles/user-1/") || r.URL.Query().Get("X-Amz-Signature") == "" {
			t.Fatalf("unexpected R2 request: %s %s", r.Method, r.URL.String())
		}
		uploaded, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	h := newMediaHandler(server.URL, "r2-access", "r2-secret", "ispilo-media", "https://media.ispilo.co.ke", "auto", "ispilolite")
	h.client = server.Client()
	h.now = func() time.Time { return time.Unix(1700000000, 0).UTC() }

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("kind", "profile")
	part, _ := writer.CreateFormFile("file", "photo.jpg")
	_, _ = part.Write([]byte("\xff\xd8\xff\xe0image"))
	_ = writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/media/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	ctx := context.WithValue(context.Background(), "userID", "user-1")
	ctx = context.WithValue(ctx, "userRole", "customer")
	rec := httptest.NewRecorder()
	h.Upload(rec, req.WithContext(ctx))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "https://media.ispilo.co.ke/ispilolite/profiles/user-1/") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if string(uploaded) != "\xff\xd8\xff\xe0image" {
		t.Fatalf("uploaded=%q", uploaded)
	}
}

func TestR2SignatureChangesWithTimestamp(t *testing.T) {
	h := testR2Handler()
	first, _, _ := h.presignedPutURL("ispilolite/profiles/user/a.jpg", time.Unix(1700000000, 0).UTC(), r2UploadTTL)
	second, _, _ := h.presignedPutURL("ispilolite/profiles/user/a.jpg", time.Unix(1700000001, 0).UTC(), r2UploadTTL)
	if first == second {
		t.Fatal("presigned URL did not change with timestamp")
	}
}
