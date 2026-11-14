// Package query provides a composable, type-safe DSL for building Elasticsearch
// request bodies. Every builder returns a plain map[string]interface{} suitable
// for marshaling to JSON and sending to ES. Builders are pure functions (no I/O)
// so they are easy to unit test and easy to compose for complex searches.
//
// Example usage:
//
//	q := Bool().
//		Must(Match("name", "safaricom").Fuzziness("AUTO")).
//		Filter(Term("county", "Nairobi"), Range("rating").Gte(3.0)).
//		Should(Match("description", "fiber").Boost(2.0)).
//		MinimumShouldMatch("1").
//		Sort(SortField("rating").Desc(), SortField("_score").Desc())
//
//	body := SearchQuery(q).From(0).Size(20).TrackTotalHits(true).Build()
package search

import (
	"fmt"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// Query clause builders
// ---------------------------------------------------------------------------

// Match builds a match query on a single field.
func Match(field, value string) *MatchQuery {
	return &MatchQuery{field: field, value: value, boost: 1.0, fuzziness: "", operator: "", minimumShouldMatch: ""}
}

type MatchQuery struct {
	field              string
	value              string
	boost              float64
	fuzziness          string
	operator           string
	minimumShouldMatch string
	analyzer           string
	zeroTermsQuery     string
	cutoffFrequency    float64
}

func (q *MatchQuery) Boost(b float64) *MatchQuery               { q.boost = b; return q }
func (q *MatchQuery) Fuzziness(f string) *MatchQuery            { q.fuzziness = f; return q }
func (q *MatchQuery) Operator(op string) *MatchQuery            { q.operator = op; return q }
func (q *MatchQuery) MinimumShouldMatch(msm string) *MatchQuery { q.minimumShouldMatch = msm; return q }
func (q *MatchQuery) Analyzer(a string) *MatchQuery             { q.analyzer = a; return q }
func (q *MatchQuery) ZeroTermsQuery(z string) *MatchQuery       { q.zeroTermsQuery = z; return q }

func (q *MatchQuery) Build() map[string]interface{} {
	inner := map[string]interface{}{"query": q.value}
	if q.boost != 1.0 {
		inner["boost"] = q.boost
	}
	if q.fuzziness != "" {
		inner["fuzziness"] = q.fuzziness
	}
	if q.operator != "" {
		inner["operator"] = q.operator
	}
	if q.minimumShouldMatch != "" {
		inner["minimum_should_match"] = q.minimumShouldMatch
	}
	if q.analyzer != "" {
		inner["analyzer"] = q.analyzer
	}
	if q.zeroTermsQuery != "" {
		inner["zero_terms_query"] = q.zeroTermsQuery
	}
	return map[string]interface{}{"match": map[string]interface{}{q.field: inner}}
}

// MultiMatch builds a multi_match query across several fields.
func MultiMatch(value string, fields ...string) *MultiMatchQuery {
	return &MultiMatchQuery{value: value, fields: fields, boost: 1.0, fuzziness: "", operator: "", minimumShouldMatch: "", type_: "", fieldsBoost: map[string]float64{}}
}

type MultiMatchQuery struct {
	value              string
	fields             []string
	boost              float64
	fuzziness          string
	operator           string
	minimumShouldMatch string
	type_              string
	analyzer           string
	fieldsBoost        map[string]float64
}

func (q *MultiMatchQuery) Boost(b float64) *MultiMatchQuery    { q.boost = b; return q }
func (q *MultiMatchQuery) Fuzziness(f string) *MultiMatchQuery { q.fuzziness = f; return q }
func (q *MultiMatchQuery) Operator(op string) *MultiMatchQuery { q.operator = op; return q }
func (q *MultiMatchQuery) MinimumShouldMatch(msm string) *MultiMatchQuery {
	q.minimumShouldMatch = msm
	return q
}
func (q *MultiMatchQuery) Type(t string) *MultiMatchQuery     { q.type_ = t; return q }
func (q *MultiMatchQuery) Analyzer(a string) *MultiMatchQuery { q.analyzer = a; return q }
func (q *MultiMatchQuery) FieldBoost(field string, b float64) *MultiMatchQuery {
	q.fieldsBoost[field] = b
	return q
}

func (q *MultiMatchQuery) Build() map[string]interface{} {
	inner := map[string]interface{}{"query": q.value, "fields": q.fields}
	if q.boost != 1.0 {
		inner["boost"] = q.boost
	}
	if q.fuzziness != "" {
		inner["fuzziness"] = q.fuzziness
	}
	if q.operator != "" {
		inner["operator"] = q.operator
	}
	if q.minimumShouldMatch != "" {
		inner["minimum_should_match"] = q.minimumShouldMatch
	}
	if q.type_ != "" {
		inner["type"] = q.type_
	}
	if q.analyzer != "" {
		inner["analyzer"] = q.analyzer
	}
	// Apply per-field boosts by converting "field" to "field^boost".
	if len(q.fieldsBoost) > 0 {
		boosted := make([]string, 0, len(q.fields))
		for _, f := range q.fields {
			if b, ok := q.fieldsBoost[f]; ok && b != 1.0 {
				boosted = append(boosted, fmt.Sprintf("%s^%s", f, strconv.FormatFloat(b, 'f', -1, 64)))
			} else {
				boosted = append(boosted, f)
			}
		}
		inner["fields"] = boosted
	}
	return map[string]interface{}{"multi_match": inner}
}

// Term builds an exact term query (keyword, numeric, boolean).
func Term(field string, value interface{}) *TermQuery {
	return &TermQuery{field: field, value: value, boost: 1.0}
}

type TermQuery struct {
	field string
	value interface{}
	boost float64
}

func (q *TermQuery) Boost(b float64) *TermQuery { q.boost = b; return q }

func (q *TermQuery) Build() map[string]interface{} {
	inner := map[string]interface{}{q.field: q.value}
	if q.boost != 1.0 {
		inner["boost"] = q.boost
	}
	return map[string]interface{}{"term": inner}
}

// Terms builds a terms query (any of several values).
func Terms(field string, values ...interface{}) *TermsQuery {
	return &TermsQuery{field: field, values: values, boost: 1.0}
}

type TermsQuery struct {
	field  string
	values []interface{}
	boost  float64
}

func (q *TermsQuery) Boost(b float64) *TermsQuery { q.boost = b; return q }

func (q *TermsQuery) Build() map[string]interface{} {
	inner := map[string]interface{}{q.field: q.values}
	if q.boost != 1.0 {
		inner["boost"] = q.boost
	}
	return map[string]interface{}{"terms": inner}
}

// Range builds a range query on a numeric or date field.
func Range(field string) *RangeQuery {
	return &RangeQuery{field: field}
}

type RangeQuery struct {
	field            string
	gt, gte, lt, lte interface{}
	boost            float64
	format           string
	timeZone         string
}

func (q *RangeQuery) Gt(v interface{}) *RangeQuery   { q.gt = v; return q }
func (q *RangeQuery) Gte(v interface{}) *RangeQuery  { q.gte = v; return q }
func (q *RangeQuery) Lt(v interface{}) *RangeQuery   { q.lt = v; return q }
func (q *RangeQuery) Lte(v interface{}) *RangeQuery  { q.lte = v; return q }
func (q *RangeQuery) Boost(b float64) *RangeQuery    { q.boost = b; return q }
func (q *RangeQuery) Format(f string) *RangeQuery    { q.format = f; return q }
func (q *RangeQuery) TimeZone(tz string) *RangeQuery { q.timeZone = tz; return q }

func (q *RangeQuery) Build() map[string]interface{} {
	inner := map[string]interface{}{}
	if q.gt != nil {
		inner["gt"] = q.gt
	}
	if q.gte != nil {
		inner["gte"] = q.gte
	}
	if q.lt != nil {
		inner["lt"] = q.lt
	}
	if q.lte != nil {
		inner["lte"] = q.lte
	}
	if q.boost != 1.0 {
		inner["boost"] = q.boost
	}
	if q.format != "" {
		inner["format"] = q.format
	}
	if q.timeZone != "" {
		inner["time_zone"] = q.timeZone
	}
	return map[string]interface{}{"range": map[string]interface{}{q.field: inner}}
}

// Bool builds a boolean query combining must/filter/should/must_not clauses.
func Bool() *BoolQuery {
	return &BoolQuery{}
}

type BoolQuery struct {
	must               []map[string]interface{}
	filter             []map[string]interface{}
	should             []map[string]interface{}
	mustNot            []map[string]interface{}
	minimumShouldMatch string
	boost              float64
}

func (q *BoolQuery) Must(clauses ...map[string]interface{}) *BoolQuery {
	q.must = append(q.must, clauses...)
	return q
}
func (q *BoolQuery) Filter(clauses ...map[string]interface{}) *BoolQuery {
	q.filter = append(q.filter, clauses...)
	return q
}
func (q *BoolQuery) Should(clauses ...map[string]interface{}) *BoolQuery {
	q.should = append(q.should, clauses...)
	return q
}
func (q *BoolQuery) MustNot(clauses ...map[string]interface{}) *BoolQuery {
	q.mustNot = append(q.mustNot, clauses...)
	return q
}
func (q *BoolQuery) MinimumShouldMatch(msm string) *BoolQuery { q.minimumShouldMatch = msm; return q }
func (q *BoolQuery) Boost(b float64) *BoolQuery               { q.boost = b; return q }

func (q *BoolQuery) Build() map[string]interface{} {
	bool := map[string]interface{}{}
	if len(q.must) > 0 {
		bool["must"] = q.must
	}
	if len(q.filter) > 0 {
		bool["filter"] = q.filter
	}
	if len(q.should) > 0 {
		bool["should"] = q.should
	}
	if len(q.mustNot) > 0 {
		bool["must_not"] = q.mustNot
	}
	if q.minimumShouldMatch != "" {
		bool["minimum_should_match"] = q.minimumShouldMatch
	}
	if q.boost != 1.0 {
		bool["boost"] = q.boost
	}
	return map[string]interface{}{"bool": bool}
}

// MatchAll builds a match_all query.
func MatchAll() map[string]interface{} {
	return map[string]interface{}{"match_all": map[string]interface{}{}}
}

// MatchNone builds a match_none query (matches nothing).
func MatchNone() map[string]interface{} {
	return map[string]interface{}{"match_none": map[string]interface{}{}}
}

// Exists builds an exists query (field must be present).
func Exists(field string) map[string]interface{} {
	return map[string]interface{}{"exists": map[string]interface{}{"field": field}}
}

// Prefix builds a prefix query.
func Prefix(field, value string) *PrefixQuery {
	return &PrefixQuery{field: field, value: value, boost: 1.0}
}

type PrefixQuery struct {
	field string
	value string
	boost float64
}

func (q *PrefixQuery) Boost(b float64) *PrefixQuery { q.boost = b; return q }

func (q *PrefixQuery) Build() map[string]interface{} {
	inner := map[string]interface{}{q.field: q.value}
	if q.boost != 1.0 {
		inner["boost"] = q.boost
	}
	return map[string]interface{}{"prefix": inner}
}

// Wildcard builds a wildcard query.
func Wildcard(field, pattern string) *WildcardQuery {
	return &WildcardQuery{field: field, pattern: pattern, boost: 1.0}
}

type WildcardQuery struct {
	field   string
	pattern string
	boost   float64
}

func (q *WildcardQuery) Boost(b float64) *WildcardQuery { q.boost = b; return q }

func (q *WildcardQuery) Build() map[string]interface{} {
	inner := map[string]interface{}{q.field: q.pattern}
	if q.boost != 1.0 {
		inner["boost"] = q.boost
	}
	return map[string]interface{}{"wildcard": inner}
}

// Fuzzy builds a fuzzy query.
func Fuzzy(field, value string) *FuzzyQuery {
	return &FuzzyQuery{field: field, value: value, boost: 1.0, fuzziness: "AUTO", maxExpansions: 50, prefixLength: 0}
}

type FuzzyQuery struct {
	field          string
	value          string
	boost          float64
	fuzziness      string
	maxExpansions  int
	prefixLength   int
	transpositions bool
}

func (q *FuzzyQuery) Boost(b float64) *FuzzyQuery       { q.boost = b; return q }
func (q *FuzzyQuery) Fuzziness(f string) *FuzzyQuery    { q.fuzziness = f; return q }
func (q *FuzzyQuery) MaxExpansions(n int) *FuzzyQuery   { q.maxExpansions = n; return q }
func (q *FuzzyQuery) PrefixLength(n int) *FuzzyQuery    { q.prefixLength = n; return q }
func (q *FuzzyQuery) Transpositions(t bool) *FuzzyQuery { q.transpositions = t; return q }

func (q *FuzzyQuery) Build() map[string]interface{} {
	inner := map[string]interface{}{"value": q.value, "fuzziness": q.fuzziness, "max_expansions": q.maxExpansions, "prefix_length": q.prefixLength}
	if q.boost != 1.0 {
		inner["boost"] = q.boost
	}
	if q.transpositions {
		inner["transpositions"] = true
	}
	return map[string]interface{}{"fuzzy": map[string]interface{}{q.field: inner}}
}

// GeoDistance builds a geo_distance filter.
func GeoDistance(field string, lat, lon float64, distance string) *GeoDistanceQuery {
	return &GeoDistanceQuery{field: field, lat: lat, lon: lon, distance: distance}
}

type GeoDistanceQuery struct {
	field    string
	lat, lon float64
	distance string
}

func (q *GeoDistanceQuery) Build() map[string]interface{} {
	return map[string]interface{}{
		"geo_distance": map[string]interface{}{
			"distance": q.distance,
			q.field:    map[string]float64{"lat": q.lat, "lon": q.lon},
		},
	}
}

// GeoBoundingBox builds a geo_bounding_box filter.
func GeoBoundingBox(field string, topLeftLat, topLeftLon, bottomRightLat, bottomRightLon float64) map[string]interface{} {
	return map[string]interface{}{
		"geo_bounding_box": map[string]interface{}{
			field: map[string]interface{}{
				"top_left":     map[string]float64{"lat": topLeftLat, "lon": topLeftLon},
				"bottom_right": map[string]float64{"lat": bottomRightLat, "lon": bottomRightLon},
			},
		},
	}
}

// IDs builds an ids query.
func IDs(values ...string) map[string]interface{} {
	return map[string]interface{}{"ids": map[string]interface{}{"values": values}}
}

// Script builds a script query.
func Script(script string, params map[string]interface{}) map[string]interface{} {
	inner := map[string]interface{}{"script": script}
	if len(params) > 0 {
		inner["params"] = params
	}
	return map[string]interface{}{"script": inner}
}

// Boosting builds a boosting query (positive query boosted, negative query reduced).
func Boosting(positive, negative map[string]interface{}, negativeBoost float64) map[string]interface{} {
	return map[string]interface{}{
		"boosting": map[string]interface{}{
			"positive":       positive,
			"negative":       negative,
			"negative_boost": negativeBoost,
		},
	}
}

// FunctionScore wraps a query with scoring functions (field_value_factor, gauss decay, etc.).
func FunctionScore(query map[string]interface{}, functions []map[string]interface{}, boostMode, scoreMode string) map[string]interface{} {
	inner := map[string]interface{}{"query": query, "functions": functions}
	if boostMode != "" {
		inner["boost_mode"] = boostMode
	}
	if scoreMode != "" {
		inner["score_mode"] = scoreMode
	}
	return map[string]interface{}{"function_score": inner}
}

// MoreLikeThis builds a more_like_this query.
func MoreLikeThis(like []map[string]interface{}, fields []string, minTermFreq, minDocFreq, maxQueryTerms int, minShouldMatch string) map[string]interface{} {
	return map[string]interface{}{
		"more_like_this": map[string]interface{}{
			"fields":               fields,
			"like":                 like,
			"min_term_freq":        minTermFreq,
			"min_doc_freq":         minDocFreq,
			"max_query_terms":      maxQueryTerms,
			"minimum_should_match": minShouldMatch,
		},
	}
}

// ---------------------------------------------------------------------------
// Sort builders
// ---------------------------------------------------------------------------

// SortField defines a sort field with order and optional mode.
type SortField struct {
	Field   string
	Order   string // "asc" | "desc"
	Mode    string // "min" | "max" | "sum" | "avg" | "median"
	Missing string // "_last" | "_first" | custom value
	Nested  *NestedSort
}

type NestedSort struct {
	Path   string
	Filter map[string]interface{}
}

// Sort builds a sort slice.
func Sort(fields ...SortField) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(fields))
	for _, f := range fields {
		entry := map[string]interface{}{}
		inner := map[string]interface{}{"order": f.Order}
		if f.Mode != "" {
			inner["mode"] = f.Mode
		}
		if f.Missing != "" {
			inner["missing"] = f.Missing
		}
		if f.Nested != nil {
			inner["nested"] = map[string]interface{}{
				"path": f.Nested.Path,
			}
			if f.Nested.Filter != nil {
				inner["nested"].(map[string]interface{})["filter"] = f.Nested.Filter
			}
		}
		entry[f.Field] = inner
		out = append(out, entry)
	}
	return out
}

// SortScore sorts by relevance score.
func SortScore(order string) map[string]interface{} {
	return map[string]interface{}{"_score": map[string]interface{}{"order": order}}
}

// SortGeoDistance sorts by geo distance.
func SortGeoDistance(field string, lat, lon float64, order, unit string) map[string]interface{} {
	return map[string]interface{}{
		"_geo_distance": map[string]interface{}{
			field:   map[string]float64{"lat": lat, "lon": lon},
			"order": order,
			"unit":  unit,
		},
	}
}

// ---------------------------------------------------------------------------
// Top-level search request builder
// ---------------------------------------------------------------------------

// SearchQuery wraps a query clause into a search request builder.
func SearchQuery(query map[string]interface{}) *SearchRequest {
	return &SearchRequest{query: query}
}

type SearchRequest struct {
	query          map[string]interface{}
	from           int
	size           int
	sort           []map[string]interface{}
	trackTotalHits bool
	trackScores    bool
	searchAfter    []interface{}
	suggest        map[string]interface{}
	aggs           map[string]interface{}
	postFilter     map[string]interface{}
	timeout        string
	searchType     string
}

func (r *SearchRequest) From(n int) *SearchRequest              { r.from = n; return r }
func (r *SearchRequest) Size(n int) *SearchRequest              { r.size = n; return r }
func (r *SearchRequest) Sort(sorts ...SortField) *SearchRequest { r.sort = Sort(sorts...); return r }
func (r *SearchRequest) SortRaw(sorts ...map[string]interface{}) *SearchRequest {
	r.sort = sorts
	return r
}
func (r *SearchRequest) TrackTotalHits(b bool) *SearchRequest { r.trackTotalHits = b; return r }
func (r *SearchRequest) TrackScores(b bool) *SearchRequest    { r.trackScores = b; return r }
func (r *SearchRequest) SearchAfter(vals ...interface{}) *SearchRequest {
	r.searchAfter = vals
	return r
}
func (r *SearchRequest) Suggest(name string, suggest map[string]interface{}) *SearchRequest {
	if r.suggest == nil {
		r.suggest = map[string]interface{}{}
	}
	r.suggest[name] = suggest
	return r
}
func (r *SearchRequest) Agg(name string, agg map[string]interface{}) *SearchRequest {
	if r.aggs == nil {
		r.aggs = map[string]interface{}{}
	}
	r.aggs[name] = agg
	return r
}
func (r *SearchRequest) PostFilter(f map[string]interface{}) *SearchRequest {
	r.postFilter = f
	return r
}
func (r *SearchRequest) Timeout(d string) *SearchRequest    { r.timeout = d; return r }
func (r *SearchRequest) SearchType(t string) *SearchRequest { r.searchType = t; return r }

func (r *SearchRequest) Build() map[string]interface{} {
	body := map[string]interface{}{"query": r.query}
	if r.from > 0 {
		body["from"] = r.from
	}
	if r.size > 0 {
		body["size"] = r.size
	}
	if len(r.sort) > 0 {
		body["sort"] = r.sort
	}
	if r.trackTotalHits {
		body["track_total_hits"] = true
	}
	if r.trackScores {
		body["track_scores"] = true
	}
	if len(r.searchAfter) > 0 {
		body["search_after"] = r.searchAfter
	}
	if len(r.suggest) > 0 {
		body["suggest"] = r.suggest
	}
	if len(r.aggs) > 0 {
		body["aggs"] = r.aggs
	}
	if r.postFilter != nil {
		body["post_filter"] = r.postFilter
	}
	if r.timeout != "" {
		body["timeout"] = r.timeout
	}
	if r.searchType != "" {
		body["search_type"] = r.searchType
	}
	return body
}

// ---------------------------------------------------------------------------
// Suggest builders
// ---------------------------------------------------------------------------

// CompletionSuggest builds a completion suggester clause.
func CompletionSuggest(name, field, prefix string, size int) map[string]interface{} {
	return map[string]interface{}{
		name: map[string]interface{}{
			"prefix":     prefix,
			"completion": map[string]interface{}{"field": field, "size": size},
		},
	}
}

// TermSuggest builds a term suggester clause (for "did you mean").
func TermSuggest(name, field, text string) map[string]interface{} {
	return map[string]interface{}{
		name: map[string]interface{}{
			"text": text,
			"term": map[string]interface{}{"field": field},
		},
	}
}

// PhraseSuggest builds a phrase suggester clause.
func PhraseSuggest(name, field, text string, size int) map[string]interface{} {
	return map[string]interface{}{
		name: map[string]interface{}{
			"text": text,
			"phrase": map[string]interface{}{
				"field": field,
				"size":  size,
				"direct_generator": []map[string]interface{}{
					{"field": field, "suggest_mode": "popular"},
				},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Aggregation builders
// ---------------------------------------------------------------------------

// TermsAgg builds a terms aggregation.
func TermsAgg(name, field string, size int) map[string]interface{} {
	inner := map[string]interface{}{"field": field}
	if size > 0 {
		inner["size"] = size
	}
	return map[string]interface{}{"terms": inner}
}

// DateHistogramAgg builds a date histogram aggregation.
func DateHistogramAgg(name, field string, interval string) map[string]interface{} {
	return map[string]interface{}{
		"date_histogram": map[string]interface{}{
			"field":             field,
			"calendar_interval": interval,
		},
	}
}

// AvgAgg builds an avg aggregation.
func AvgAgg(name, field string) map[string]interface{} {
	return map[string]interface{}{"avg": map[string]interface{}{"field": field}}
}

// ---------------------------------------------------------------------------
// Highlight builder
// ---------------------------------------------------------------------------

// Highlight builds a highlight clause.
func Highlight(fields ...string) map[string]interface{} {
	hfields := map[string]interface{}{}
	for _, f := range fields {
		hfields[f] = map[string]interface{}{}
	}
	return map[string]interface{}{"fields": hfields}
}

// SanitizeQuery trims whitespace from all string leaf values in a query body
// so empty strings don't get sent to ES as invalid clauses.
func SanitizeQuery(body map[string]interface{}) map[string]interface{} {
	sanitizeValue(body)
	return body
}

func sanitizeValue(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		for k, val := range x {
			x[k] = sanitizeValue(val)
		}
	case []interface{}:
		for i, val := range x {
			x[i] = sanitizeValue(val)
		}
	case string:
		if strings.TrimSpace(x) == "" {
			return nil
		}
	}
	return v
}

// StringOrNil returns s if non-empty, otherwise nil. Useful for optional query
// fields that should be omitted when empty.
func StringOrNil(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}
