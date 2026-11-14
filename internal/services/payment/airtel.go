package payment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"ispilolite/internal/models"
)

// AirtelProvider uses configurable OAuth and collection endpoints because
// Airtel endpoint versions differ by merchant account and country.
type AirtelProvider struct {
	baseURL, clientID, clientSecret, callbackURL, webhookSecret string
	client                                                      *http.Client
}

func NewAirtelProvider(baseURL, clientID, clientSecret, callbackURL, webhookSecret string) *AirtelProvider {
	return &AirtelProvider{baseURL: strings.TrimRight(baseURL, "/"), clientID: clientID, clientSecret: clientSecret,
		callbackURL: callbackURL, webhookSecret: webhookSecret, client: &http.Client{Timeout: 20 * time.Second}}
}
func (p *AirtelProvider) Name() string { return "airtel" }

func (p *AirtelProvider) Initiate(ctx context.Context, in InitiateRequest) (*InitiateResult, error) {
	if p.baseURL == "" || p.clientID == "" || p.clientSecret == "" {
		return nil, ErrProvider
	}
	token, err := p.token(ctx)
	if err != nil {
		return nil, err
	}
	payload := map[string]interface{}{"reference": in.Reference, "subscriber": map[string]string{"country": "KE", "currency": in.Currency, "msisdn": normalizeKenyanPhone(in.Phone)}, "transaction": map[string]interface{}{"amount": in.Amount, "country": "KE", "currency": in.Currency, "id": in.Reference}, "callback_url": p.callbackURL}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/merchant/v1/payments/", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Country", "KE")
	req.Header.Set("X-Currency", in.Currency)
	response, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	var result map[string]interface{}
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	_ = json.Unmarshal(responseBody, &result)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: Airtel initiation failed", ErrProvider)
	}
	return &InitiateResult{ExternalID: in.Reference, Status: "pending", Raw: result}, nil
}

func (p *AirtelProvider) token(ctx context.Context) (string, error) {
	body, _ := json.Marshal(map[string]string{"client_id": p.clientID, "client_secret": p.clientSecret, "grant_type": "client_credentials"})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/auth/oauth2/token", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	response, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	var result struct {
		AccessToken string `json:"access_token"`
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result) != nil || result.AccessToken == "" {
		return "", ErrProvider
	}
	return result.AccessToken, nil
}

func (p *AirtelProvider) ParseWebhook(header http.Header, payload []byte) (*models.PaymentProviderEvent, error) {
	if p.webhookSecret == "" || (!verifyHMAC(p.webhookSecret, payload, header.Get("X-Signature")) && header.Get("X-Webhook-Token") != p.webhookSecret) {
		return nil, ErrForbidden
	}
	var data struct {
		Transaction struct {
			ID      string  `json:"id"`
			Status  string  `json:"status"`
			Amount  float64 `json:"amount"`
			Message string  `json:"message"`
		} `json:"transaction"`
		Reference string `json:"reference"`
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		return nil, err
	}
	external := data.Transaction.ID
	if external == "" {
		external = data.Reference
	}
	if external == "" {
		return nil, ErrInvalid
	}
	status := "failed"
	if strings.EqualFold(data.Transaction.Status, "success") || strings.EqualFold(data.Transaction.Status, "successful") {
		status = "succeeded"
	}
	return &models.PaymentProviderEvent{EventID: external + ":" + data.Transaction.Status, ExternalID: external, Status: status, Amount: data.Transaction.Amount, FailureReason: data.Transaction.Message}, nil
}
