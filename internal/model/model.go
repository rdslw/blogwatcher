package model

import "time"

const (
	InterestStatePrefer = "prefer"
	InterestStateNormal = "normal"
	InterestStateHide   = "hide"
)

func IsValidInterestState(value string) bool {
	switch value {
	case InterestStatePrefer, InterestStateNormal, InterestStateHide:
		return true
	default:
		return false
	}
}

type Blog struct {
	ID             int64
	Name           string
	URL            string
	FeedURL        string
	ScrapeSelector string
	LastScanned    *time.Time
}

type Article struct {
	ID             int64
	BlogID         int64
	Title          string
	URL            string
	PublishedDate  *time.Time
	DiscoveredDate *time.Time
	IsRead         bool
	Summary        string
	SummaryEngine  string
	InterestState  string
	InterestReason string
	InterestEngine string
	InterestJudged *time.Time
}
