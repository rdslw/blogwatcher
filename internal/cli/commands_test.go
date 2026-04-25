package cli

import (
	"testing"

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
