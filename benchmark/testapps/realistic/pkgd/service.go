// Package pkgd wires pkga/pkgb/pkgc together into a small "service" layer,
// standing in for the kind of handler-calls-business-logic-calls-utility
// layering a real Go backend has.
package pkgd

import (
	"fmt"
	"strings"

	"whackbench/testapps/realistic/pkga"
	"whackbench/testapps/realistic/pkgb"
	"whackbench/testapps/realistic/pkgc"
)

// Service bundles a request cache with the string/math helpers.
type Service struct {
	cache *pkga.LRUCache
}

// New creates a Service with a modestly-sized request cache.
func New() *Service {
	return &Service{cache: pkga.New(128)}
}

// Report describes the computed response for a given input phrase.
type Report struct {
	Slug      string
	Title     string
	WordCount int
	Primes    []int
	Fib       []int
	Stats     pkgc.Stats
	CacheHit  bool
}

// Analyze runs phrase through the string/math helpers and caches the word
// count keyed by slug, demonstrating a (fake) cross-layer cache hit path.
func (s *Service) Analyze(phrase string) Report {
	slug := pkgb.Slugify(phrase)

	wc, hit := s.cache.Get(slug)
	if !hit {
		wc = pkgb.WordCount(phrase)
		s.cache.Put(slug, wc)
	}

	vals := make([]float64, 0, len(phrase))
	for _, r := range phrase {
		vals = append(vals, float64(r))
	}

	return Report{
		Slug:      slug,
		Title:     pkgb.TitleCase(phrase),
		WordCount: wc,
		Primes:    pkgc.Primes(50),
		Fib:       pkgc.Fibonacci(10),
		Stats:     pkgc.Summarize(vals),
		CacheHit:  hit,
	}
}

// String renders a Report as a human-readable summary line.
func (r Report) String() string {
	primeStrs := make([]string, len(r.Primes))
	for i, p := range r.Primes {
		primeStrs[i] = fmt.Sprintf("%d", p)
	}
	return fmt.Sprintf(
		"slug=%s title=%q words=%d cacheHit=%v primes=[%s] meanRune=%.2f",
		r.Slug, r.Title, r.WordCount, r.CacheHit, strings.Join(primeStrs, ","), r.Stats.Mean,
	)
}
