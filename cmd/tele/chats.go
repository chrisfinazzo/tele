package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"

	"github.com/sorokin-vladimir/tele/internal/core/project"
	"github.com/sorokin-vladimir/tele/internal/domain"
)

// newChatsCmd is `tele chats`: the chats of the account, a line each (#181).
func newChatsCmd(o *options) *cobra.Command {
	var q chatsQuery
	var archived bool
	cmd := &cobra.Command{
		Use:   "chats",
		Short: "List the chats",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if archived {
				q.folder = domain.ArchiveFolderID
			}
			return askAccount(cmd.Context(), *o, func(ctx context.Context, acc accountSurface) error {
				return listChats(ctx, acc, cmd.OutOrStdout(), q)
			})
		},
	}
	f := cmd.Flags()
	f.BoolVar(&q.asJSON, "json", false, "print a JSON object a chat, as docs/cli.md describes")
	f.BoolVar(&q.unread, "unread", false, "only the chats with something unread")
	f.BoolVar(&archived, "archived", false, "the archived chats instead of the main list")
	f.IntVar(&q.folder, "folder", 0, "the chats of the folder with this id instead of the main list")
	cmd.MarkFlagsMutuallyExclusive("archived", "folder")
	return cmd
}

// chatsQuery is what `tele chats` was asked for.
type chatsQuery struct {
	// folder is the list to print: 0 for the main list,
	// domain.ArchiveFolderID for the archive, or a folder's id.
	folder int
	unread bool
	asJSON bool
}

// listChats prints the chats of the list q names, in its order: as text, or
// as JSON Lines. The list is the one a window would show, folder rules and
// all, since only the owner holds what they are judged on.
func listChats(ctx context.Context, acc accountSurface, out io.Writer, q chatsQuery) error {
	d, err := firstDelta(ctx, acc, project.ChatListWindow{Folder: q.folder, Limit: math.MaxInt32})
	if err != nil {
		return err
	}
	list := d.ChatList
	if list == nil {
		return errors.New("the account answered with something other than its chats")
	}
	if q.folder != 0 && q.folder != domain.ArchiveFolderID && !hasFolder(list.Folders.Filters, q.folder) {
		return &exitCode{code: notFoundCode, msg: fmt.Sprintf(
			"There is no folder %d. The folders are:\n%s", q.folder, strings.TrimSuffix(foldersText(list.Folders.Filters), "\n"))}
	}
	var rows []project.ChatRow
	for _, c := range list.Rows {
		if !q.unread || c.Unread > 0 || c.UnreadMark {
			rows = append(rows, c)
		}
	}
	if q.asJSON {
		return chatsJSON(out, rows)
	}
	_, err = io.WriteString(out, chatsText(rows))
	return err
}

func hasFolder(filters []domain.FolderFilter, id int) bool {
	for _, f := range filters {
		if f.ID == id {
			return true
		}
	}
	return false
}

// foldersText is a line per folder the account has, its id and title. All
// Chats and the Archive are not folders here: they are the list and
// --archived.
func foldersText(filters []domain.FolderFilter) string {
	var b strings.Builder
	for _, f := range filters {
		if f.ID == 0 || f.ID == domain.ArchiveFolderID {
			continue
		}
		fmt.Fprintf(&b, "  %d %s\n", f.ID, f.Title)
	}
	if b.Len() == 0 {
		return "  none\n"
	}
	return b.String()
}

// chatJSON is a chat as `tele chats --json` prints it. Its fields are a
// contract kept within a major version (docs/cli.md), so it is its own type
// rather than ChatRow with tags.
type chatJSON struct {
	ID       int64            `json:"id"`
	Title    string           `json:"title"`
	Username string           `json:"username,omitempty"`
	Kind     project.ChatKind `json:"kind"`
	// Unread is a forum's topics with unread, as its badge counts them.
	Unread     int  `json:"unread"`
	Mentions   int  `json:"mentions"`
	Reactions  int  `json:"reactions"`
	UnreadMark bool `json:"unread_mark"`
	Muted      bool `json:"muted"`
	Archived   bool `json:"archived"`
	Forum      bool `json:"forum"`
}

// chatsJSON writes rows to out as JSON Lines, an object a chat.
func chatsJSON(out io.Writer, rows []project.ChatRow) error {
	enc := json.NewEncoder(out)
	for _, c := range rows {
		err := enc.Encode(chatJSON{
			ID: c.ID, Title: c.Title, Username: c.Username, Kind: c.Kind,
			Unread: c.Unread, Mentions: c.Mentions, Reactions: c.Reactions,
			UnreadMark: c.UnreadMark, Muted: c.Muted, Archived: c.Archived, Forum: c.IsForum,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// chatsText is a line per chat: its id, its title and its badges, in columns.
func chatsText(rows []project.ChatRow) string {
	lines := make([]columnLine, 0, len(rows))
	for _, c := range rows {
		lines = append(lines, columnLine{id: fmt.Sprint(c.ID), title: c.Title, badges: chatBadges(c)})
	}
	return columnsText(lines)
}

// columnLine is one line of a listing: what it lists by id and title, and its
// badges.
type columnLine struct {
	id, title, badges string
}

// columnsText lays lines out in columns, measured as the terminal draws them.
// A line with no badges ends at its title, so none carries trailing spaces.
func columnsText(lines []columnLine) string {
	idW, titleW := 0, 0
	for _, l := range lines {
		idW = max(idW, len(l.id))
		titleW = max(titleW, ansi.StringWidth(l.title))
	}
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.id)
		b.WriteString(strings.Repeat(" ", idW-len(l.id)+2))
		b.WriteString(l.title)
		if l.badges != "" {
			b.WriteString(strings.Repeat(" ", titleW-ansi.StringWidth(l.title)+2))
			b.WriteString(l.badges)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// chatBadges says what the chat list would badge a chat with. A forum's unread
// count is of its topics, as Telegram Desktop badges one.
func chatBadges(c project.ChatRow) string {
	var bs []string
	if c.IsForum {
		bs = append(bs, "forum")
		if c.Unread > 0 {
			bs = append(bs, count(c.Unread, "topic")+" unread")
		}
	} else if c.Unread > 0 {
		bs = append(bs, fmt.Sprintf("%d unread", c.Unread))
	}
	if c.Mentions > 0 {
		bs = append(bs, count(c.Mentions, "mention"))
	}
	if c.Reactions > 0 {
		bs = append(bs, count(c.Reactions, "reaction"))
	}
	if c.UnreadMark {
		bs = append(bs, "marked unread")
	}
	if c.Muted {
		bs = append(bs, "muted")
	}
	return strings.Join(bs, ", ")
}

// count is n of noun, the noun made plural for any n but one.
func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
