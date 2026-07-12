package cli

import (
	"strings"
	"testing"

	"github.com/rdslw/blogwatcher/internal/model"
	"github.com/rdslw/blogwatcher/internal/storage"
)

func TestFormatInterestStats(t *testing.T) {
	tests := []struct {
		name  string
		stats storage.ArticleStats
		want  string
	}{
		{
			name:  "no unread",
			stats: storage.ArticleStats{Total: 3, Unread: 0, Hide: 0, Normal: 0, Prefer: 0},
			want:  "none h/n/p",
		},
		{
			name:  "unread without interest data",
			stats: storage.ArticleStats{Total: 3, Unread: 2, Hide: 0, Normal: 0, Prefer: 0},
			want:  "no interest data",
		},
		{
			name:  "partial unread interest data",
			stats: storage.ArticleStats{Total: 5, Unread: 4, Hide: 2, Normal: 1, Prefer: 0},
			want:  "partial interest data",
		},
		{
			name:  "unread interest buckets",
			stats: storage.ArticleStats{Total: 5, Unread: 4, Hide: 2, Normal: 1, Prefer: 1},
			want:  "2/1/1 h/n/p",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatInterestStats(tt.stats); got != tt.want {
				t.Fatalf("formatInterestStats() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoadBlogOverviewsUnreadOnly(t *testing.T) {
	db, err := storage.OpenDatabase(t.TempDir() + "/blogwatcher.db")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	unreadBlog, err := db.AddBlog(model.Blog{Name: "Unread", URL: "https://unread.example.com"})
	if err != nil {
		t.Fatalf("add unread blog: %v", err)
	}
	emptyBlog, err := db.AddBlog(model.Blog{Name: "Empty", URL: "https://empty.example.com"})
	if err != nil {
		t.Fatalf("add empty blog: %v", err)
	}
	if _, err := db.AddArticle(model.Article{BlogID: unreadBlog.ID, Title: "Unread", URL: "https://unread.example.com/1"}); err != nil {
		t.Fatalf("add article: %v", err)
	}

	overviews, err := loadBlogOverviews(db, []model.Blog{emptyBlog, unreadBlog}, true)
	if err != nil {
		t.Fatalf("load blog overviews: %v", err)
	}
	if len(overviews) != 1 || overviews[0].blog.ID != unreadBlog.ID || overviews[0].stats.Unread != 1 {
		t.Fatalf("unexpected unread blogs: %+v", overviews)
	}

	all, err := loadBlogOverviews(db, []model.Blog{emptyBlog, unreadBlog}, false)
	if err != nil {
		t.Fatalf("load all blog overviews: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected both blogs without --unread, got %+v", all)
	}
}

func TestSinceCannotCombineWithArticleIDs(t *testing.T) {
	for _, args := range [][]string{
		{"articles", "--since", "7", "42"},
		{"summary", "--since", "7", "42"},
		{"interest", "--since", "7", "42"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			cmd := NewRootCommand()
			cmd.SetArgs(args)
			err := cmd.Execute()
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), "cannot combine --since with article IDs") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
