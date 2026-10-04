package tg

import (
	"testing"

	"github.com/gotd/td/tg"
	"github.com/sorokin-vladimir/tele/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInputMediaFromMessageMedia_Photo(t *testing.T) {
	mm := &tg.MessageMediaPhoto{}
	mm.SetPhoto(&tg.Photo{ID: 11, AccessHash: 22, FileReference: []byte{1, 2, 3}})

	got, err := inputMediaFromMessageMedia(mm)
	require.NoError(t, err)

	photo, ok := got.(*tg.InputMediaPhoto)
	require.True(t, ok, "got %T, want *tg.InputMediaPhoto", got)
	ref, ok := photo.ID.(*tg.InputPhoto)
	require.True(t, ok)
	assert.Equal(t, int64(11), ref.ID)
	assert.Equal(t, int64(22), ref.AccessHash)
	assert.Equal(t, []byte{1, 2, 3}, ref.FileReference)
}

func TestInputMediaFromMessageMedia_Document(t *testing.T) {
	mm := &tg.MessageMediaDocument{}
	mm.SetDocument(&tg.Document{ID: 33, AccessHash: 44, FileReference: []byte{9}})

	got, err := inputMediaFromMessageMedia(mm)
	require.NoError(t, err)

	doc, ok := got.(*tg.InputMediaDocument)
	require.True(t, ok, "got %T, want *tg.InputMediaDocument", got)
	ref, ok := doc.ID.(*tg.InputDocument)
	require.True(t, ok)
	assert.Equal(t, int64(33), ref.ID)
	assert.Equal(t, int64(44), ref.AccessHash)
	assert.Equal(t, []byte{9}, ref.FileReference)
}

func TestInputMediaFromMessageMedia_Unsupported(t *testing.T) {
	_, err := inputMediaFromMessageMedia(&tg.MessageMediaEmpty{})
	assert.ErrorIs(t, err, ErrUnsupportedAlbumMedia)
}

func TestBuildSendMultiMediaRequest_CaptionOnFirstItemOnly(t *testing.T) {
	items := []AlbumItem{
		{Media: &tg.InputMediaPhoto{}, Caption: "hello"},
		{Media: &tg.InputMediaPhoto{}},
	}
	req := buildSendMultiMediaRequest(&tg.InputPeerEmpty{}, items, []int64{1, 2}, 0, 0)

	require.Len(t, req.MultiMedia, 2)
	assert.Equal(t, "hello", req.MultiMedia[0].Message)
	assert.Equal(t, "", req.MultiMedia[1].Message)
	assert.Equal(t, int64(1), req.MultiMedia[0].RandomID)
	assert.Equal(t, int64(2), req.MultiMedia[1].RandomID)
}

func TestBuildSendMultiMediaRequest_WithReply(t *testing.T) {
	items := []AlbumItem{{Media: &tg.InputMediaPhoto{}}}
	req := buildSendMultiMediaRequest(&tg.InputPeerEmpty{}, items, []int64{1}, 42, 0)

	reply, ok := req.ReplyTo.(*tg.InputReplyToMessage)
	require.True(t, ok, "got %T, want *tg.InputReplyToMessage", req.ReplyTo)
	assert.Equal(t, 42, reply.ReplyToMsgID)
}

func TestBuildSendMultiMediaRequest_WithoutReply(t *testing.T) {
	items := []AlbumItem{{Media: &tg.InputMediaPhoto{}}}
	req := buildSendMultiMediaRequest(&tg.InputPeerEmpty{}, items, []int64{1}, 0, 0)
	assert.Nil(t, req.ReplyTo)
}

func TestBuildSendMultiMediaRequest_Entities(t *testing.T) {
	items := []AlbumItem{{
		Media:    &tg.InputMediaPhoto{},
		Caption:  "bold",
		Entities: []domain.MessageEntity{{Type: "bold", Offset: 0, Length: 4}},
	}}
	req := buildSendMultiMediaRequest(&tg.InputPeerEmpty{}, items, []int64{1}, 0, 0)
	require.Len(t, req.MultiMedia[0].Entities, 1)
	assert.IsType(t, &tg.MessageEntityBold{}, req.MultiMedia[0].Entities[0])
}
