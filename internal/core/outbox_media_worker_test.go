package core

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sorokin-vladimir/tele/internal/domain"
	"github.com/sorokin-vladimir/tele/internal/telerr"
)

func TestWorker_ASinglePartGoesOutWithSendMediaAndSkipsUploadMedia(t *testing.T) {
	c := &stubClient{}
	o, _ := newCmdOwner(t, c)
	o.SetOutbox(newOutboxStore(t))
	ctx := runWorker(t, o)

	require.NoError(t, o.SendMedia(ctx, MediaSendRequest{
		Ref: "r1", ChatID: 1, Files: []MediaFile{{Path: writeFile(t, "a.jpg", 4)}},
	}))

	waitFor(t, "the part was never sent", func() bool { return c.sendMediaCalls() == 1 })
	assert.Zero(t, c.uploadMediaCalls(),
		"a lone media needs no uploadMedia hop; only sendMultiMedia refuses uploaded input")
}

func TestWorker_AGroupUploadsEveryPartThenSendsOneAlbum(t *testing.T) {
	c := &stubClient{}
	o, _ := newCmdOwner(t, c)
	o.SetOutbox(newOutboxStore(t))
	ctx := runWorker(t, o)

	require.NoError(t, o.SendMedia(ctx, MediaSendRequest{
		Ref: "r1", ChatID: 1, Files: []MediaFile{
			{Path: writeFile(t, "a.jpg", 4)},
			{Path: writeFile(t, "b.jpg", 4)},
		},
	}))

	waitFor(t, "the album was never sent", func() bool { return c.albumItemCount() == 2 })
	assert.Len(t, c.uploads(), 2)
	assert.Equal(t, 2, c.uploadMediaCalls())
	assert.Zero(t, c.sendMediaCalls(), "a group of two is an album, not two messages")
}

func TestWorker_AlbumRandomIDsAreDerivedFromTheRef(t *testing.T) {
	c := &stubClient{}
	o, _ := newCmdOwner(t, c)
	o.SetOutbox(newOutboxStore(t))
	ctx := runWorker(t, o)

	require.NoError(t, o.SendMedia(ctx, MediaSendRequest{
		Ref: "r1", ChatID: 1, Files: []MediaFile{
			{Path: writeFile(t, "a.jpg", 4)},
			{Path: writeFile(t, "b.jpg", 4)},
		},
	}))

	waitFor(t, "the album was never sent", func() bool { return c.albumItemCount() == 2 })
	assert.Equal(t,
		[]int64{mediaRandomID("r1#0", 0), mediaRandomID("r1#0", 1)},
		c.randomIDs(),
		"Telegram deduplicates on these; a fresh one per attempt is how a retry arrives twice")
}

func TestWorker_AppliesTheRefreshedMessagesAndDropsTheEntry(t *testing.T) {
	c := &stubClient{}
	o, st := newCmdOwner(t, c)
	q := newOutboxStore(t)
	o.SetOutbox(q)
	ctx := runWorker(t, o)

	require.NoError(t, o.SendMedia(ctx, MediaSendRequest{
		Ref: "r1", ChatID: 1, Files: []MediaFile{
			{Path: writeFile(t, "a.jpg", 4)},
			{Path: writeFile(t, "b.jpg", 4)},
		},
	}))

	waitFor(t, "the album never reached the store", func() bool { return len(st.Messages(domain.HistoryKey{ChatID: 1})) == 2 })
	waitFor(t, "the entry outlived its messages", func() bool {
		_, still := q.Get("r1#0")
		return !still
	})
	msgs := st.Messages(domain.HistoryKey{ChatID: 1})
	assert.Equal(t, int64(42), msgs[0].GroupedID,
		"the refresh is what carries grouped_id; without it the parts never collapse")
}

func TestWorker_AMissingFileFailsTerminallyAndNamesIt(t *testing.T) {
	c := &stubClient{}
	o, _ := newCmdOwner(t, c)
	q := newOutboxStore(t)
	o.SetOutbox(q)
	path := writeFile(t, "gone.jpg", 4)
	require.NoError(t, o.SendMedia(context.Background(), MediaSendRequest{
		Ref: "r1", ChatID: 1, Files: []MediaFile{{Path: path}},
	}))
	require.NoError(t, os.Remove(path))
	runWorker(t, o)

	waitFor(t, "the entry never failed", func() bool {
		e, ok := q.Get("r1#0")
		return ok && e.State == domain.OutboxFailed
	})
	e, _ := q.Get("r1#0")
	assert.Equal(t, telerr.NotFound, e.ErrKind)
	assert.Contains(t, e.ErrDetail, "gone.jpg")
	assert.Zero(t, c.sendMediaCalls())
}

func TestWorker_ARefusedPartTakesTheWholeGroupDown(t *testing.T) {
	c := &stubClient{uploadErr: &telerr.Error{Kind: telerr.Forbidden, Detail: "FILE_PARTS_INVALID"}}
	o, _ := newCmdOwner(t, c)
	q := newOutboxStore(t)
	o.SetOutbox(q)
	ctx := runWorker(t, o)

	require.NoError(t, o.SendMedia(ctx, MediaSendRequest{
		Ref: "r1", ChatID: 1, Files: []MediaFile{
			{Path: writeFile(t, "a.jpg", 4)},
			{Path: writeFile(t, "b.jpg", 4)},
		},
	}))

	waitFor(t, "the entry never failed", func() bool {
		e, ok := q.Get("r1#0")
		return ok && e.State == domain.OutboxFailed
	})
	assert.Zero(t, c.albumItemCount(),
		"the group is the unit: it lands whole or waits for a decision")
	e, _ := q.Get("r1#0")
	require.NotNil(t, e.Media, "the paths stay, so a retry has something to re-upload")
	assert.Len(t, e.Media.Parts, 2)
}

func TestWorker_PublishesAggregateProgressAcrossTheGroup(t *testing.T) {
	c := &stubClient{}
	o, _ := newCmdOwner(t, c)
	o.SetOutbox(newOutboxStore(t))
	ctx := runWorker(t, o)

	require.NoError(t, o.SendMedia(ctx, MediaSendRequest{
		Ref: "r1", ChatID: 1, Files: []MediaFile{
			{Path: writeFile(t, "a.jpg", 100)},
			{Path: writeFile(t, "b.jpg", 100)},
		},
	}))

	var seen Progress
	waitFor(t, "no progress was published for the second part", func() bool {
		for {
			select {
			case p := <-o.Progress():
				if p.Part == 2 {
					seen = p
					return true
				}
			default:
				return false
			}
		}
	})
	assert.Equal(t, 2, seen.Parts)
	assert.Equal(t, int64(200), seen.Total, "Total is the whole entry, not one file")
	assert.Greater(t, seen.Done, int64(100), "the first part's bytes are already counted")
}

func TestDiscardOutbox_StopsAnUploadInFlightAndRecordsNoFailure(t *testing.T) {
	c := &stubClient{uploadBlock: make(chan struct{})}
	o, _ := newCmdOwner(t, c)
	q := newOutboxStore(t)
	o.SetOutbox(q)
	ctx := runWorker(t, o)

	require.NoError(t, o.SendMedia(ctx, MediaSendRequest{
		Ref: "r1", ChatID: 1, Files: []MediaFile{{Path: writeFile(t, "big.mp4", 4)}},
	}))
	waitFor(t, "the upload never started", func() bool { return len(c.uploads()) == 1 })
	// Queued behind the upload. One worker drains the queue, so this cannot go
	// until the discarded upload has actually let the goroutine go — which is
	// what makes this a test of the cancel rather than of the delete.
	require.NoError(t, o.Send(ctx, SendRequest{Ref: "t1", ChatID: 1, Text: "after"}))

	require.NoError(t, o.DiscardOutbox("r1#0"))

	waitFor(t, "the worker never got past the discarded upload", func() bool {
		return c.sendCalls() == 1
	})
	_, still := q.Get("r1#0")
	assert.False(t, still, "the discarded entry must be gone")
	assert.Zero(t, c.sendMediaCalls(), "a cancelled upload must not be sent")
	// Give the worker a moment to do the wrong thing, if it is going to.
	time.Sleep(20 * time.Millisecond)
	e, resurrected := q.Get("r1#0")
	assert.False(t, resurrected, "a discarded entry must not come back as a failure: %v", e.ErrKind)
}

func TestWorker_AnUploadingEntryIsVisibleAsSuch(t *testing.T) {
	c := &stubClient{uploadBlock: make(chan struct{})}
	o, _ := newCmdOwner(t, c)
	q := newOutboxStore(t)
	o.SetOutbox(q)
	t.Cleanup(func() { close(c.uploadBlock) })
	ctx := runWorker(t, o)

	require.NoError(t, o.SendMedia(ctx, MediaSendRequest{
		Ref: "r1", ChatID: 1, Files: []MediaFile{{Path: writeFile(t, "big.mp4", 4)}},
	}))

	waitFor(t, "the entry never went into uploading", func() bool {
		e, ok := q.Get("r1#0")
		return ok && e.State == domain.OutboxUploading
	})
}
