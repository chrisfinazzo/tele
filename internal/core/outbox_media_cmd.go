package core

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"go.uber.org/zap"

	"github.com/sorokin-vladimir/tele/internal/core/outbox"
	"github.com/sorokin-vladimir/tele/internal/domain"
	"github.com/sorokin-vladimir/tele/internal/media"
	"github.com/sorokin-vladimir/tele/internal/telerr"
)

// maxAlbumParts is Telegram's cap on the number of media in one grouped album.
const maxAlbumParts = 10

// MediaFile is one local file a client wants sent.
type MediaFile struct {
	Path string
	// SendAs is the user's explicit choice, as in "send this photo as a file".
	// Zero leaves the decision to the owner, which detects it from the MIME type.
	SendAs domain.MediaKind
}

// MediaSendRequest is one composer submission: the files as staged, plus the
// caption that belongs to the submission as a whole.
type MediaSendRequest struct {
	Ref          string
	ChatID       int64
	Files        []MediaFile
	Caption      string
	Entities     []domain.MessageEntity
	ReplyToMsgID int
	// TopicID is the forum topic the message was composed in, 0 outside a
	// forum (#275).
	TopicID int
}

// SendMedia puts local files on the durable queue and returns once they are on
// disk. It does not wait for Telegram, and it does not upload: what happens
// after this is the queue's business.
//
// Telegram takes at most ten media in one album and will not mix visual media
// with documents, so one submission becomes one entry per album group, with refs
// derived from the caller's. The group is what Telegram sends atomically, so it
// is what the queue schedules, retries and discards.
//
// It fails synchronously on a chat it cannot address and on a file it cannot
// read, and queues nothing in either case: a submission is one user action, and
// half of it is not a useful outcome. Everything discoverable only later is
// reported through the entry, because a client that has been acknowledged must
// not have to stay alive to learn the outcome.
func (o *Owner) SendMedia(_ context.Context, req MediaSendRequest) error {
	if o.outbox == nil {
		return &telerr.Error{Kind: telerr.Internal, Op: "outbox.sendmedia", Detail: "no outbox configured"}
	}
	// Resolved here to fail fast, and again on every attempt: a peer can become
	// addressable later, but one that is unknown now is a caller's mistake.
	if _, err := o.peer(req.ChatID); err != nil {
		return err
	}
	parts, err := describeFiles(req.Files)
	if err != nil {
		return err
	}
	if len(parts) == 0 {
		return &telerr.Error{Kind: telerr.Internal, Op: "outbox.sendmedia", Detail: "no files"}
	}

	now := time.Now()
	for i, group := range partitionMedia(parts) {
		ref := req.Ref + "#" + strconv.Itoa(i)
		payload := &domain.OutboxMediaSend{Parts: group}
		// Telegram renders an album's caption from its first part, and the
		// caption belongs to the submission rather than to a group: it rides on
		// the first group only, as does the reply.
		if i == 0 {
			payload.Caption = req.Caption
			payload.Entities = req.Entities
			payload.ReplyToMsgID = req.ReplyToMsgID
		}
		added, isNew, err := o.outbox.Add(domain.OutboxEntry{
			Ref:       ref,
			ChatID:    req.ChatID,
			TopicID:   req.TopicID,
			RandomID:  outbox.RandomIDFor(ref),
			Kind:      domain.OutboxMedia,
			State:     domain.OutboxQueued,
			Media:     payload,
			CreatedAt: now,
		})
		if err != nil {
			return &telerr.Error{Kind: telerr.Internal, Op: "outbox.sendmedia", Detail: err.Error(), Cause: err}
		}
		if !isNew {
			continue
		}
		o.log.Debug("outbox: queued media",
			zap.String("ref", added.Ref), zap.Int64("chat_id", added.ChatID),
			zap.Int("parts", len(group)))
	}
	o.Refresh()
	o.wakeOutbox()
	return nil
}

// describeFiles stats each file so the entry describes itself to a client that
// was not running when it was queued. SendAs is carried through untouched: it is
// the user's intent, not a detection result.
func describeFiles(files []MediaFile) ([]domain.OutboxMediaPart, error) {
	out := make([]domain.OutboxMediaPart, 0, len(files))
	for _, f := range files {
		info, err := os.Stat(f.Path)
		if err != nil {
			return nil, &telerr.Error{
				Kind:   telerr.NotFound,
				Op:     "outbox.sendmedia",
				Detail: "cannot read " + filepath.Base(f.Path),
				Cause:  err,
			}
		}
		out = append(out, domain.OutboxMediaPart{
			Path:   f.Path,
			Name:   filepath.Base(f.Path),
			Size:   info.Size(),
			SendAs: f.SendAs,
		})
	}
	return out, nil
}

// albumClass is a part's album compatibility class. Telegram refuses to mix
// visual media with documents in one grouped album, so parts of different
// classes must go out as separate albums.
type albumClass int

const (
	classVisual   albumClass = iota // photo, video
	classDocument                   // documents, and anything sent as a file
)

// albumClassOf classifies by the "send as" choice where there is one, so a photo
// the user chose to send as a file travels with the documents.
func albumClassOf(p domain.OutboxMediaPart) albumClass {
	kind := p.SendAs
	if kind == 0 {
		if mime, err := media.DetectMIME(p.Path); err == nil {
			kind = media.DefaultMediaType(mime)
		}
	}
	switch kind {
	case domain.MediaPhoto, domain.MediaVideo:
		return classVisual
	default:
		return classDocument
	}
}

// partitionMedia splits parts into album-sized groups: stable by class (group
// order follows each class's first occurrence, order within a class is
// preserved), then chunked at maxAlbumParts. Both the mixed-type case and the
// more-than-ten case fall out of the same pass.
func partitionMedia(parts []domain.OutboxMediaPart) [][]domain.OutboxMediaPart {
	if len(parts) == 0 {
		return nil
	}
	var order []albumClass
	buckets := make(map[albumClass][]domain.OutboxMediaPart, 2)
	for _, p := range parts {
		c := albumClassOf(p)
		if _, seen := buckets[c]; !seen {
			order = append(order, c)
		}
		buckets[c] = append(buckets[c], p)
	}
	var out [][]domain.OutboxMediaPart
	for _, c := range order {
		bucket := buckets[c]
		for start := 0; start < len(bucket); start += maxAlbumParts {
			end := start + maxAlbumParts
			if end > len(bucket) {
				end = len(bucket)
			}
			out = append(out, bucket[start:end])
		}
	}
	return out
}

// mediaRandomID is the deduplication key for one part of an entry. Derived like
// the text one, so a resubmission in the window before anything was written down
// still produces the same values and Telegram deduplicates a repeated album.
func mediaRandomID(ref string, part int) int64 {
	return outbox.RandomIDFor(ref + "#" + strconv.Itoa(part))
}
