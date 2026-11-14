package payment

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ispilolite/internal/models"
)

type MPesaProvider struct {
	baseURL, consumerKey, consumerSecret, shortCode, passKey, callbackURL, webhookSecret string
	client                                                                               *http.Client
}

func NewMPesaProvider(baseURL, key, secret, shortCode, passKey, callbackURL, webhookSecret string) *MPesaProvider {
	return &MPesaProvider{baseURL: strings.TrimRight(baseURL, "/"), consumerKey: key, consumerSecret: secret,
		shortCode: shortCode, passKey: passKey, callbackURL: callbackURL, webhookSecret: webhookSecret,
		client: &http.Client{Timeout: 20 * time.Second}}
}

func (p *MPesaProvider) Name() string { return "mpesa" }

func (p *MPesaProvider) Initiate(ctx context.Context, in InitiateRequest) (*InitiateResult, error) {
	if p.baseURL == "" || p.consumerKey == "" || p.consumerSecret == "" || p.shortCode == "" || p.passKey == "" || p.callbackURL == "" {
		return nil, ErrProvider
	}
	token, err := p.token(ctx)
	if err != nil {
		return nil, err
	}
	timestamp := time.Now().Format("20060102150405")
	password := base64.StdEncoding.EncodeToString([]byte(p.shortCode + p.passKey + timestamp))
	payload := map[string]interface{}{"BusinessShortCode": p.shortCode, "Password": password, "Timestamp": timestamp,
		"TransactionType": "CustomerPayBillOnline", "Amount": int64(in.Amount + .5), "PartyA": normalizeKenyanPhone(in.Phone),
		"PartyB": p.shortCode, "PhoneNumber": normalizeKenyanPhone(in.Phone), "CallBackURL": p.callbackURL,
		"AccountReference": in.Reference, "TransactionDesc": in.Description}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/mpesa/stkpush/v1/processrequest", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	response, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	var result map[string]interface{}
	if json.Unmarshal(responseBody, &result) != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: M-Pesa initiation failed", ErrProvider)
	}
	externalID, _ := result["CheckoutRequestID"].(string)
	if externalID == "" {
		return nil, fmt.Errorf("%w: M-Pesa response missing checkout id", ErrProvider)
	}
	return &InitiateResult{ExternalID: externalID, Status: "pending", Raw: result}, nil
}

func (p *MPesaProvider) token(ctx context.Context) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/oauth/v1/generate?grant_type=client_credentials", nil)
	req.SetBasicAuth(p.consumerKey, p.consumerSecret)
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

func (p *MPesaProvider) ParseWebhook(header http.Header, payload []byte) (*models.PaymentProviderEvent, error) {
	// Deployments should put the configured token in the callback URL and have
	// the reverse proxy copy it to X-Webhook-Token. HMAC is also accepted.
	if p.webhookSecret == "" || (header.Get("X-Webhook-Token") != p.webhookSecret && !verifyHMAC(p.webhookSecret, payload, header.Get("X-Signature"))) {
		return nil, ErrForbidden
	}
	var envelope struct {
		Body struct {
			Callback struct {
				CheckoutRequestID string `json:"CheckoutRequestID"`
				ResultCode        int    `json:"ResultCode"`
				ResultDesc        string `json:"ResultDesc"`
				Metadata          struct {
					Items []struct {
						Name  string      `json:"Name"`
						Value interface{} `json:"Value"`
					} `json:"Item"`
				} `json:"CallbackMetadata"`
			} `json:"stkCallback"`
		} `json:"Body"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, err
	}
	callback := envelope.Body.Callback
	if callback.CheckoutRequestID == "" {
		return nil, ErrInvalid
	}
	event := &models.PaymentProviderEvent{EventID: callback.CheckoutRequestID + ":" + strconv.Itoa(callback.ResultCode), ExternalID: callback.CheckoutRequestID, Status: "failed", FailureReason: callback.ResultDesc}
	if callback.ResultCode == 0 {
		event.Status, event.FailureReason = "succeeded", ""
	}
	for _, item := range callback.Metadata.Items {
		switch item.Name {
		case "Amount":
			switch value := item.Value.(type) {
			case float64:
				event.Amount = value
			}
		case "MpesaReceiptNumber":
			event.Receipt = fmt.Sprint(item.Value)
		}
	}
	return event, nil
}

func normalizeKenyanPhone(value string) string {
	value = strings.NewReplacer("+", "", " ", "", "-", "").Replace(value)
	if strings.HasPrefix(value, "0") {
		return "254" + strings.TrimPrefix(value, "0")
	}
	return value
}
