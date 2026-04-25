package controller

import (
	"testing"
	"time"

	"github.com/rdslw/blogwatcher/internal/model"
)

func ts(s string) *time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return &t
}

func ids(articles []model.Article) []int64 {
	out := make([]int64, len(articles))
	for i, a := range articles {
		out[i] = a.ID
	}
	return out
}

func equalIDs(got, want []int64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestParseSortOrder(t *testing.T) {
	cases := []struct {
		in      string
		want    SortOrder
		wantErr bool
	}{
		{"", SortNewestFirst, false},
		{"newest", SortNewestFirst, false},
		{"new", SortNewestFirst, false},
		{"desc", SortNewestFirst, false},
		{"oldest", SortOldestFirst, false},
		{"old", SortOldestFirst, false},
		{"asc", SortOldestFirst, false},
		{"earliest", SortOldestFirst, false},
		{"random", SortNewestFirst, true},
		{"NEWEST", SortNewestFirst, true}, // case-sensitive: red case
	}
	for _, c := range cases {
		got, err := ParseSortOrder(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseSortOrder(%q): expected error, got nil", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseSortOrder(%q): unexpected error: %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("ParseSortOrder(%q): got %v, want %v", c.in, got, c.want)
		}
	}
}

func TestSortArticlesByPublishedDate(t *testing.T) {
	articles := []model.Article{
		{ID: 1, PublishedDate: ts("2024-03-01")},
		{ID: 2, PublishedDate: ts("2024-01-15")},
		{ID: 3, PublishedDate: ts("2024-05-10")},
	}

	SortArticles(articles, SortOldestFirst)
	if !equalIDs(ids(articles), []int64{2, 1, 3}) {
		t.Fatalf("oldest first: got %v, want [2 1 3]", ids(articles))
	}

	SortArticles(articles, SortNewestFirst)
	if !equalIDs(ids(articles), []int64{3, 1, 2}) {
		t.Fatalf("newest first: got %v, want [3 1 2]", ids(articles))
	}
}

func TestSortArticlesNilDateFallsBackToDiscovered(t *testing.T) {
	articles := []model.Article{
		{ID: 1, DiscoveredDate: ts("2024-02-01")},
		{ID: 2, PublishedDate: ts("2024-01-15")},
		{ID: 3, DiscoveredDate: ts("2024-04-01")},
	}

	SortArticles(articles, SortOldestFirst)
	if !equalIDs(ids(articles), []int64{2, 1, 3}) {
		t.Fatalf("oldest with nil published: got %v, want [2 1 3]", ids(articles))
	}
}

func TestSortArticlesStableByIDOnEqualDates(t *testing.T) {
	d := ts("2024-01-01")
	articles := []model.Article{
		{ID: 30, PublishedDate: d},
		{ID: 10, PublishedDate: d},
		{ID: 20, PublishedDate: d},
	}
	SortArticles(articles, SortOldestFirst)
	if !equalIDs(ids(articles), []int64{10, 20, 30}) {
		t.Fatalf("equal dates: got %v, want [10 20 30]", ids(articles))
	}
}

func TestSortSummaryAndInterestResults(t *testing.T) {
	mk := func(id int64, date string) model.Article {
		return model.Article{ID: id, PublishedDate: ts(date)}
	}

	sums := []SummaryResult{
		{Article: mk(1, "2024-03-01")},
		{Article: mk(2, "2024-01-15")},
		{Article: mk(3, "2024-05-10")},
	}
	SortSummaryResults(sums, SortOldestFirst)
	gotIDs := []int64{sums[0].Article.ID, sums[1].Article.ID, sums[2].Article.ID}
	if !equalIDs(gotIDs, []int64{2, 1, 3}) {
		t.Fatalf("summary oldest first: got %v, want [2 1 3]", gotIDs)
	}

	ints := []InterestResult{
		{Article: mk(1, "2024-03-01")},
		{Article: mk(2, "2024-01-15")},
		{Article: mk(3, "2024-05-10")},
	}
	SortInterestResults(ints, SortNewestFirst)
	gotIDs = []int64{ints[0].Article.ID, ints[1].Article.ID, ints[2].Article.ID}
	if !equalIDs(gotIDs, []int64{3, 1, 2}) {
		t.Fatalf("interest newest first: got %v, want [3 1 2]", gotIDs)
	}
}

func TestSortArticlesEmptyAndSingle(t *testing.T) {
	// Should not panic.
	SortArticles(nil, SortOldestFirst)
	SortArticles([]model.Article{}, SortNewestFirst)
	one := []model.Article{{ID: 7, PublishedDate: ts("2024-01-01")}}
	SortArticles(one, SortOldestFirst)
	if one[0].ID != 7 {
		t.Fatalf("single: got %d, want 7", one[0].ID)
	}
}
