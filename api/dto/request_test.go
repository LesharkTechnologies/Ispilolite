package dto_test

import (
	"testing"

	"ispilolite/api/dto"
)

func TestSearchParamsOffset(t *testing.T) {
	tests := []struct {
		name     string
		params   dto.SearchParams
		expected int
	}{
		{
			name:     "default zero values",
			params:   dto.SearchParams{},
			expected: 0,
		},
		{
			name:     "page 0 with page size 5 (like CI integration test)",
			params:   dto.SearchParams{Query: "CI", PageSize: 5},
			expected: 0,
		},
		{
			name:     "page 1 with page size 10",
			params:   dto.SearchParams{Page: 1, PageSize: 10},
			expected: 0,
		},
		{
			name:     "page 2 with page size 10",
			params:   dto.SearchParams{Page: 2, PageSize: 10},
			expected: 10,
		},
		{
			name:     "page 3 with page size 20",
			params:   dto.SearchParams{Page: 3, PageSize: 20},
			expected: 40,
		},
		{
			name:     "negative page with page size 5",
			params:   dto.SearchParams{Page: -1, PageSize: 5},
			expected: 0,
		},
		{
			name:     "page 2 with negative page size",
			params:   dto.SearchParams{Page: 2, PageSize: -5},
			expected: 0,
		},
		{
			name:     "valid cursor overrides page",
			params:   dto.SearchParams{Page: 2, PageSize: 10, Cursor: dto.EncodeCursor(25)},
			expected: 25,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := tc.params.Offset()
			if actual != tc.expected {
				t.Fatalf("expected offset %d, got %d", tc.expected, actual)
			}
		})
	}
}

func TestSearchParamsNormalize(t *testing.T) {
	p := dto.SearchParams{Query: "CI", PageSize: 5}
	p.Normalize()

	if p.Page != 1 {
		t.Fatalf("expected page to normalize to 1, got %d", p.Page)
	}
	if p.PageSize != 5 {
		t.Fatalf("expected page size to remain 5, got %d", p.PageSize)
	}

	empty := dto.SearchParams{}
	empty.Normalize()
	if empty.Page != 1 {
		t.Fatalf("expected default page 1, got %d", empty.Page)
	}
	if empty.PageSize != 20 {
		t.Fatalf("expected default page size 20, got %d", empty.PageSize)
	}
}
