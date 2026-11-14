package payment

import (
	"context"
	"net/http"
	"testing"
)

func TestMPesaWebhookRequiresSecret(t *testing.T) {
	provider := NewMPesaProvider("", "", "", "", "", "", "secret")
	payload := []byte(`{"Body":{"stkCallback":{"CheckoutRequestID":"ws_CO_1","ResultCode":0,"ResultDesc":"ok","CallbackMetadata":{"Item":[{"Name":"Amount","Value":100},{"Name":"MpesaReceiptNumber","Value":"ABC"}]}}}}`)
	if _, err := provider.ParseWebhook(http.Header{}, payload); err == nil {
		t.Fatal("unsigned webhook accepted")
	}
	header := http.Header{}
	header.Set("X-Webhook-Token", "secret")
	event, err := provider.ParseWebhook(header, payload)
	if err != nil {
		t.Fatalf("valid webhook rejected: %v", err)
	}
	if event.Status != "succeeded" || event.Amount != 100 || event.Receipt != "ABC" {
		t.Fatalf("unexpected event: %#v", event)
	}
}

func TestPaymentWebhooksRejectEmptyConfiguredSecret(t *testing.T) {
	for name, provider := range map[string]Provider{
		"mpesa":  NewMPesaProvider("", "", "", "", "", "", ""),
		"airtel": NewAirtelProvider("", "", "", "", ""),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := provider.ParseWebhook(http.Header{}, []byte(`{}`)); err == nil {
				t.Fatal("callback accepted with no configured secret and no authentication")
			}
		})
	}
	t.Setenv("PAYMENT_WEBHOOK_SECRET", "")
	if err := NewService(nil, nil, nil).ConfirmSettlement(context.Background(), http.Header{}, []byte(`{}`)); err == nil {
		t.Fatal("settlement accepted with no configured secret")
	}
}
