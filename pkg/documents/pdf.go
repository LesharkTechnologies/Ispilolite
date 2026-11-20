package documents

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"ispilolite/internal/models"
)

type Store struct{ Root string }

func NewStore() *Store {
	root := strings.TrimSpace(os.Getenv("DOCUMENT_STORAGE_ROOT"))
	if root == "" {
		root = "storage/documents"
	}
	return &Store{Root: root}
}

func (s *Store) SaveQuotation(q *models.Quotation) (*models.Document, error) {
	if err := os.MkdirAll(s.Root, 0750); err != nil {
		return nil, err
	}
	name := q.ID + ".pdf"
	path := filepath.Join(s.Root, name)
	if err := os.WriteFile(path, quotationPDF(q), 0600); err != nil {
		return nil, err
	}
	return &models.Document{ID: uuid.NewString(), OwnerID: q.IssuerID, QuotationID: q.ID,
		StoragePath: path, FileName: "quotation-" + q.QuotationNumber + ".pdf",
		ContentType: "application/pdf", Visibility: "PRIVATE", CreatedAt: q.CreatedAt}, nil
}

// quotationPDF creates a dependency-free, multi-page PDF. It intentionally
// does not fetch CompanyLogoURL: remote media is client-uploaded to R2
// and the API avoids server-side URL fetching/SSRF. The URL remains on the
// quotation record and can be rendered by the client.
func quotationPDF(q *models.Quotation) []byte {
	lines := quotationLines(q)
	const linesPerPage = 42
	pageCount := (len(lines) + linesPerPage - 1) / linesPerPage
	if pageCount < 1 {
		pageCount = 1
	}

	fontObject := 3 + pageCount
	firstContentObject := fontObject + 1
	objects := make([]string, 0, 2+pageCount*2+1)
	objects = append(objects, "<< /Type /Catalog /Pages 2 0 R >>")
	kids := make([]string, pageCount)
	for i := 0; i < pageCount; i++ {
		kids[i] = fmt.Sprintf("%d 0 R", 3+i)
	}
	objects = append(objects, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), pageCount))
	for i := 0; i < pageCount; i++ {
		contentObject := firstContentObject + i
		objects = append(objects, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 %d 0 R >> >> /Contents %d 0 R >>", fontObject, contentObject))
	}
	objects = append(objects, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	for page := 0; page < pageCount; page++ {
		start := page * linesPerPage
		end := start + linesPerPage
		if end > len(lines) {
			end = len(lines)
		}
		pageLines := lines[start:end]
		var stream strings.Builder
		stream.WriteString("BT /F1 10 Tf 14 TL 50 752 Td ")
		for i, line := range pageLines {
			if i > 0 {
				stream.WriteString("T* ")
			}
			stream.WriteString("(" + pdfEscape(line) + ") Tj ")
		}
		stream.WriteString("ET")
		content := stream.String()
		objects = append(objects, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))
	}
	return buildPDF(objects)
}

func quotationLines(q *models.Quotation) []string {
	created := q.CreatedAt
	if created.IsZero() {
		created = time.Now().UTC()
	}
	lines := []string{
		"ISPILO LITE - QUOTATION",
		"Quotation: " + q.QuotationNumber,
		"Date: " + created.Format("02 Jan 2006"),
		"Status: " + q.Status,
		"",
		"FROM",
		truncate(q.CompanyName, 72),
		truncate(strings.Join(nonEmpty(q.CompanyPhone, q.CompanyEmail), " | "), 90),
		truncate(q.CompanyAddress, 90),
		"",
		"TO",
		truncate(q.CustomerName, 72),
		truncate(strings.Join(nonEmpty(q.CustomerPhone, q.CustomerEmail), " | "), 90),
		truncate(q.CustomerLocation, 90),
		"",
		"ITEMS",
		"Description                              Qty       Unit       Amount",
	}
	for _, item := range q.Items {
		if item == nil {
			continue
		}
		line := fmt.Sprintf("%-38s %8s %10s %12s", truncate(item.Item, 38),
			formatNumber(item.Quantity), moneyText(item.UnitPrice), moneyText(item.Amount))
		lines = append(lines, line)
		if item.Description != "" {
			lines = append(lines, "  "+truncate(item.Description, 82))
		}
	}
	lines = append(lines, "", "Subtotal: "+q.Currency+" "+moneyText(q.Subtotal))
	if q.DiscountAmount > 0 {
		lines = append(lines, "Discount: "+q.Currency+" "+moneyText(q.DiscountAmount))
	}
	if q.TransportEnabled {
		lines = append(lines, "Transport: "+q.Currency+" "+moneyText(q.TransportAmount))
	}
	if q.TaxEnabled {
		lines = append(lines, fmt.Sprintf("Tax (%.2f%%): %s %s", q.TaxRate, q.Currency, moneyText(q.TaxAmount)))
	}
	lines = append(lines, "TOTAL: "+q.Currency+" "+moneyText(q.TotalAmount), "Payment: "+q.PaymentMethod)
	if len(q.Terms) > 0 {
		lines = append(lines, "", "TERMS")
		for i, term := range q.Terms {
			lines = append(lines, strconv.Itoa(i+1)+". "+truncate(term, 88))
		}
	}
	if q.Notes != "" {
		lines = append(lines, "", "NOTES", truncate(q.Notes, 88))
	}
	lines = append(lines, "", "Public reference: "+q.PublicCode)
	return lines
}

func buildPDF(objects []string) []byte {
	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for i, object := range objects {
		offsets[i+1] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for i := 1; i < len(offsets); i++ {
		fmt.Fprintf(&out, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return out.Bytes()
}

func pdfEscape(value string) string {
	value = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if r > unicode.MaxASCII || (!unicode.IsPrint(r) && r != ' ') {
			return '?'
		}
		return r
	}, value)
	return strings.NewReplacer("\\", "\\\\", "(", "\\(", ")", "\\)").Replace(value)
}

func truncate(value string, max int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= max {
		return string(runes)
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}

func moneyText(value float64) string { return fmt.Sprintf("%.2f", value) }

func formatNumber(value float64) string {
	if value == float64(int64(value)) {
		return strconv.FormatInt(int64(value), 10)
	}
	return strconv.FormatFloat(value, 'f', 2, 64)
}

func nonEmpty(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}
