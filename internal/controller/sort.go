package controller

import (
	"fmt"
	"sort"
	"time"

	"github.com/rdslw/blogwatcher/internal/model"
)

// SortOrder controls the chronological ordering of article output.
type SortOrder int

const (
	// SortNewestFirst orders articles by date with the newest first (default).
	SortNewestFirst SortOrder = iota
	// SortOldestFirst orders articles by date with the earliest first.
	SortOldestFirst
)

// ParseSortOrder converts a CLI flag value to a SortOrder. Matching is
// case-sensitive; accepted values are "newest" (or "new"/"desc"; default)
// and "oldest" (or "old"/"asc"/"earliest").
func ParseSortOrder(value string) (SortOrder, error) {
	switch value {
	case "", "newest", "new", "desc":
		return SortNewestFirst, nil
	case "oldest", "old", "asc", "earliest":
		return SortOldestFirst, nil
	default:
		return SortNewestFirst, fmt.Errorf("invalid --sort value: %q (must be 'newest' or 'oldest')", value)
	}
}

// SortArticles sorts articles in place by published date, falling back to
// discovered date and then ID for stable ordering.
func SortArticles(articles []model.Article, order SortOrder) {
	sort.SliceStable(articles, func(i, j int) bool {
		return articleLess(articles[i], articles[j], order)
	})
}

// SortSummaryResults sorts summary results in place using the same ordering
// rules as SortArticles, keyed on the embedded Article.
func SortSummaryResults(results []SummaryResult, order SortOrder) {
	sort.SliceStable(results, func(i, j int) bool {
		return articleLess(results[i].Article, results[j].Article, order)
	})
}

// SortInterestResults sorts interest results in place using the same ordering
// rules as SortArticles, keyed on the embedded Article.
func SortInterestResults(results []InterestResult, order SortOrder) {
	sort.SliceStable(results, func(i, j int) bool {
		return articleLess(results[i].Article, results[j].Article, order)
	})
}

func articleSortDate(a model.Article) time.Time {
	if a.PublishedDate != nil {
		return *a.PublishedDate
	}
	if a.DiscoveredDate != nil {
		return *a.DiscoveredDate
	}
	return time.Time{}
}

func articleLess(a, b model.Article, order SortOrder) bool {
	da, db := articleSortDate(a), articleSortDate(b)
	if !da.Equal(db) {
		if order == SortOldestFirst {
			return da.Before(db)
		}
		return da.After(db)
	}
	return a.ID < b.ID
}
