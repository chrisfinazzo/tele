package tg

import (
	"testing"

	"github.com/gotd/td/tg"
	"github.com/sorokin-vladimir/tele/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertToTGEntitiesNameMention(t *testing.T) {
	es := []domain.MessageEntity{
		{Type: "mention_name", Offset: 0, Length: 5, UserID: 10, AccessHash: 99},
		{Type: "mention", Offset: 6, Length: 4}, // username: no entity emitted
	}
	out := convertToTGEntities(es)
	if len(out) != 1 {
		t.Fatalf("want 1 tg entity, got %d", len(out))
	}
	mn, ok := out[0].(*tg.InputMessageEntityMentionName)
	if !ok {
		t.Fatalf("want *tg.InputMessageEntityMentionName, got %T", out[0])
	}
	iu, ok := mn.UserID.(*tg.InputUser)
	if !ok {
		t.Fatalf("want *tg.InputUser, got %T", mn.UserID)
	}
	if mn.Offset != 0 || mn.Length != 5 || iu.UserID != 10 || iu.AccessHash != 99 {
		t.Fatalf("unexpected: %+v / %+v", mn, iu)
	}
}

func TestBuildSendRequestSetsEntities(t *testing.T) {
	es := []domain.MessageEntity{{Type: "mention_name", Offset: 0, Length: 3, UserID: 1, AccessHash: 2}}
	req := buildSendRequest(&tg.InputPeerEmpty{}, "abc", 7, 0, 0, es)
	if len(req.Entities) != 1 {
		t.Fatalf("want 1 entity in request, got %d", len(req.Entities))
	}
}

func TestBuildSendRequestNoEntities(t *testing.T) {
	req := buildSendRequest(&tg.InputPeerEmpty{}, "hi", 7, 0, 0, nil)
	if len(req.Entities) != 0 {
		t.Fatalf("want 0 entities, got %d", len(req.Entities))
	}
}

func TestConvertToTGEntitiesMapsAllTypes(t *testing.T) {
	es := []domain.MessageEntity{
		{Type: "bold", Offset: 0, Length: 1},
		{Type: "italic", Offset: 1, Length: 1},
		{Type: "strike", Offset: 2, Length: 1},
		{Type: "underline", Offset: 3, Length: 1},
		{Type: "code", Offset: 4, Length: 1},
		{Type: "pre", Offset: 5, Length: 1, Language: "go"},
		{Type: "text_url", Offset: 6, Length: 1, URL: "https://ya.ru"},
	}
	out := convertToTGEntities(es)
	require.Len(t, out, 7)
	assert.IsType(t, &tg.MessageEntityBold{}, out[0])
	assert.IsType(t, &tg.MessageEntityItalic{}, out[1])
	assert.IsType(t, &tg.MessageEntityStrike{}, out[2])
	assert.IsType(t, &tg.MessageEntityUnderline{}, out[3])
	assert.IsType(t, &tg.MessageEntityCode{}, out[4])

	pre, ok := out[5].(*tg.MessageEntityPre)
	require.True(t, ok)
	assert.Equal(t, "go", pre.Language)

	link, ok := out[6].(*tg.MessageEntityTextURL)
	require.True(t, ok)
	assert.Equal(t, "https://ya.ru", link.URL)
}

// Auto-detected types are found server-side; sending them back would be noise.
func TestConvertToTGEntitiesSkipsServerDetectedTypes(t *testing.T) {
	es := []domain.MessageEntity{
		{Type: "url", Offset: 0, Length: 5},
		{Type: "hashtag", Offset: 6, Length: 3},
		{Type: "email", Offset: 10, Length: 5},
	}
	assert.Empty(t, convertToTGEntities(es))
}

func TestBuildSendMediaRequestSetsEntities(t *testing.T) {
	es := []domain.MessageEntity{{Type: "bold", Offset: 0, Length: 3}}
	req := buildSendMediaRequest(&tg.InputPeerEmpty{}, &tg.InputMediaEmpty{}, "abc", 7, 0, 0, es)
	require.Len(t, req.Entities, 1)
	assert.IsType(t, &tg.MessageEntityBold{}, req.Entities[0])
}

func TestBuildSendMediaRequestNoEntities(t *testing.T) {
	req := buildSendMediaRequest(&tg.InputPeerEmpty{}, &tg.InputMediaEmpty{}, "hi", 7, 0, 0, nil)
	assert.Empty(t, req.Entities)
}

func TestBuildEditRequestSetsEntities(t *testing.T) {
	es := []domain.MessageEntity{{Type: "bold", Offset: 0, Length: 3}}
	req := buildEditRequest(&tg.InputPeerEmpty{}, 42, "abc", es)
	assert.Equal(t, 42, req.ID)
	assert.Equal(t, "abc", req.Message)
	require.Len(t, req.Entities, 1)
	assert.IsType(t, &tg.MessageEntityBold{}, req.Entities[0])
}

func TestBuildEditRequestNoEntities(t *testing.T) {
	req := buildEditRequest(&tg.InputPeerEmpty{}, 42, "abc", nil)
	assert.Empty(t, req.Entities)
}
