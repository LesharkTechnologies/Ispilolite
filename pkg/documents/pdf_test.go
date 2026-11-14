package documents

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"ispilolite/internal/models"
)

func TestQuotationPDFAppearance(t *testing.T) {
	pdf := quotationPDF(&models.Quotation{QuotationNumber: "QT-1", CompanyName: "Fast Fiber",
		CustomerName: "Amina", TotalAmount: 1250, Subtotal: 1250, Currency: "KES", CreatedAt: time.Now(),
		Items: []*models.QuotationItem{{Item: "Router installation", Quantity: 1, UnitPrice: 1250, Amount: 1250}}})
	if !bytes.HasPrefix(pdf, []byte("%PDF-1.4")) {
		t.Fatal("quotation output is not a PDF")
	}
	if !strings.Contains(string(pdf), "QT-1") {
		t.Fatal("quotation number missing from PDF")
	}
	if !strings.Contains(string(pdf), "Router installation") || !strings.Contains(string(pdf), "Amina") {
		t.Fatal("quotation details missing from PDF")
	}
}
