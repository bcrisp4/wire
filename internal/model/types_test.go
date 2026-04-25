package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestEntry_ZeroValueIsValid(t *testing.T) {
	e := Entry{}
	assert.Zero(t, e.ID)
	assert.False(t, e.Read)
	assert.Nil(t, e.PublishedAt)
}

func TestFeed_FieldsCompile(t *testing.T) {
	now := time.Now().Unix()
	f := Feed{
		ID:           1,
		UserID:       1,
		Title:        "Example",
		FeedURL:      "https://example.com/feed",
		PollInterval: 3600,
		NextPollAt:   &now,
	}
	assert.Equal(t, "Example", f.Title)
	assert.NotNil(t, f.NextPollAt)
}

func TestUser_ZeroValueIsValid(t *testing.T) {
	u := User{}
	assert.Zero(t, u.ID)
	assert.Empty(t, u.Username)
}
