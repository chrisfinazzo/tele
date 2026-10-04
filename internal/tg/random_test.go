package tg

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/sorokin-vladimir/tele/internal/domain"
)

func TestNewRandomID_IsNonZeroAndVaries(t *testing.T) {
	a, b := NewRandomID(), NewRandomID()

	assert.NotZero(t, a)
	assert.NotZero(t, b)
	assert.NotEqual(t, a, b)
}

// sendMessageFunc is the shape SendMessage has to keep: a trailing random_id
// the caller owns. Named so the assertion below states a contract rather than
// restating an inferable type.
type sendMessageFunc func(context.Context, domain.Peer, string, int, int, []domain.MessageEntity, int64) (domain.Message, error)

// The send methods must take random_id from the caller. Generating it inside
// the WithRetry closure meant every retry carried a fresh one, which Telegram
// cannot deduplicate: a retried send arrived twice (#193). These assertions are
// compile-time, because the signature is the guarantee.
func TestSendMethodsTakeTheRandomIDFromTheCaller(t *testing.T) {
	var c *GotdClient

	var _ sendMessageFunc = c.SendMessage

	var mediaParams SendMediaParams
	mediaParams.RandomID = 1

	var albumParams SendAlbumParams
	albumParams.RandomIDs = []int64{1}

	assert.Equal(t, int64(1), mediaParams.RandomID)
	assert.Equal(t, []int64{1}, albumParams.RandomIDs)
}
