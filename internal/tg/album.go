package tg

import (
	"context"
	"errors"
	"fmt"

	"github.com/gotd/td/tg"
	"github.com/sorokin-vladimir/tele/internal/domain"
	"github.com/sorokin-vladimir/tele/internal/telerr"
	"go.uber.org/zap"
)

// ErrUnsupportedAlbumMedia is returned when messages.uploadMedia answers with a
// media type an album part cannot be built from (anything but photo/document).
var ErrUnsupportedAlbumMedia = errors.New("unsupported album media")

// inputMediaFromMessageMedia converts the MessageMedia that messages.uploadMedia
// returns into the InputMedia that messages.sendMultiMedia accepts. The album
// method rejects raw inputMediaUploaded* constructors, so this hop is mandatory
// for every album part.
func inputMediaFromMessageMedia(mm tg.MessageMediaClass) (tg.InputMediaClass, error) {
	switch m := mm.(type) {
	case *tg.MessageMediaPhoto:
		p, ok := m.GetPhoto()
		if !ok {
			return nil, ErrUnsupportedAlbumMedia
		}
		photo, ok := p.(*tg.Photo)
		if !ok {
			return nil, ErrUnsupportedAlbumMedia
		}
		return &tg.InputMediaPhoto{ID: &tg.InputPhoto{
			ID:            photo.ID,
			AccessHash:    photo.AccessHash,
			FileReference: photo.FileReference,
		}}, nil
	case *tg.MessageMediaDocument:
		d, ok := m.GetDocument()
		if !ok {
			return nil, ErrUnsupportedAlbumMedia
		}
		doc, ok := d.(*tg.Document)
		if !ok {
			return nil, ErrUnsupportedAlbumMedia
		}
		return &tg.InputMediaDocument{ID: &tg.InputDocument{
			ID:            doc.ID,
			AccessHash:    doc.AccessHash,
			FileReference: doc.FileReference,
		}}, nil
	default:
		return nil, fmt.Errorf("%w: %T", ErrUnsupportedAlbumMedia, mm)
	}
}

// UploadMedia turns an uploaded InputFile (wrapped in an inputMediaUploaded*
// constructor) into a server-side media ref usable as an album part.
func (c *GotdClient) UploadMedia(ctx context.Context, peer domain.Peer, media tg.InputMediaClass) (tg.InputMediaClass, error) {
	api, err := c.acquireAPI()
	if err != nil {
		return nil, err
	}
	inputPeer := peerToInput(peer)
	var out tg.InputMediaClass
	err = WithRetry(ctx, func() error {
		mm, err := api.MessagesUploadMedia(ctx, &tg.MessagesUploadMediaRequest{
			Peer:  inputPeer,
			Media: media,
		})
		if err != nil {
			c.log.Error("MessagesUploadMedia failed", zap.Error(err))
			return err
		}
		converted, err := inputMediaFromMessageMedia(mm)
		if err != nil {
			return err
		}
		out = converted
		return nil
	})
	return out, err
}

// AlbumItem is one part of a grouped album. Media must already be a server-side
// ref produced by UploadMedia. Telegram shows the album's caption from its first
// part, so only the first item carries Caption/Entities.
type AlbumItem struct {
	Media    tg.InputMediaClass
	Caption  string
	Entities []domain.MessageEntity
}

// SendAlbumParams carries everything SendAlbum needs.
type SendAlbumParams struct {
	Peer         domain.Peer
	Items        []AlbumItem
	ReplyToMsgID int
	// TopicID is the forum topic the send lands in, 0 outside a forum (#275).
	TopicID int
	// RandomIDs is one deduplication key per item, in item order. They belong to
	// the caller and must stay the same across every retry of one logical send:
	// Telegram deduplicates on them, so fresh ones per attempt are how a retried
	// album arrives twice (#193).
	RandomIDs []int64
}

func buildSendMultiMediaRequest(inputPeer tg.InputPeerClass, items []AlbumItem, randomIDs []int64, replyToMsgID, topicID int) *tg.MessagesSendMultiMediaRequest {
	multi := make([]tg.InputSingleMedia, 0, len(items))
	for i, it := range items {
		single := tg.InputSingleMedia{
			Media:    it.Media,
			RandomID: randomIDs[i],
			Message:  it.Caption,
		}
		if ent := convertToTGEntities(it.Entities); len(ent) > 0 {
			single.SetEntities(ent)
		}
		multi = append(multi, single)
	}
	req := &tg.MessagesSendMultiMediaRequest{
		Peer:       inputPeer,
		MultiMedia: multi,
	}
	if r := replyHeader(replyToMsgID, topicID); r != nil {
		req.ReplyTo = r
	}
	return req
}

// SendAlbum sends items as one grouped album (messages.sendMultiMedia) and
// returns the confirmed message IDs in item order. Every returned ID is
// suppressed, exactly as SendMedia does, so the echoed update does not paint a
// duplicate bubble.
func (c *GotdClient) SendAlbum(ctx context.Context, p SendAlbumParams) ([]int, error) {
	if len(p.Items) == 0 {
		return nil, nil
	}
	api, err := c.acquireAPI()
	if err != nil {
		return nil, err
	}
	if len(p.RandomIDs) != len(p.Items) {
		return nil, &telerr.Error{
			Kind:   telerr.Internal,
			Op:     "messages.sendMultiMedia",
			Detail: "random id count does not match item count",
		}
	}
	c.traceLog.Debug("SendAlbum", zap.Int64("peer_id", p.Peer.ID), zap.Int("parts", len(p.Items)))
	inputPeer := peerToInput(p.Peer)
	var realIDs []int
	err = WithRetry(ctx, func() error {
		// The random IDs are the caller's and stay put across attempts. They
		// used to be regenerated here, on the belief that a retried batch must
		// not reuse the failed attempt's IDs — which is backwards: reusing them
		// is exactly what stops Telegram from accepting the batch twice (#193).
		updates, err := api.MessagesSendMultiMedia(ctx, buildSendMultiMediaRequest(inputPeer, p.Items, p.RandomIDs, p.ReplyToMsgID, p.TopicID))
		if err != nil {
			c.log.Error("MessagesSendMultiMedia failed", zap.Error(err))
			return err
		}
		ids := extractSentMessageIDs(updates, p.RandomIDs)
		c.suppressMu.Lock()
		for _, id := range ids {
			if id != 0 {
				c.suppressIDs[id] = struct{}{}
			}
		}
		c.suppressMu.Unlock()
		realIDs = ids
		c.traceLog.Debug("SendAlbum ok", zap.Int64("peer_id", p.Peer.ID), zap.Ints("real_ids", ids))
		return nil
	})
	return realIDs, err
}
