package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"

	"github.com/sorokin-vladimir/tele/internal/core/project"
)

// newChatsCmd is `tele chats`: the chats of the account, a line each (#181).
func newChatsCmd(o *options) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "chats",
		Short: "List the chats",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return askAccount(cmd.Context(), *o, func(ctx context.Context, acc accountSurface) error {
				return listChats(ctx, acc, cmd.OutOrStdout(), asJSON)
			})
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print a JSON object a chat, as docs/cli.md describes")
	return cmd
}

// listChats prints the chats of the main list, in its order: as text, or as
// JSON Lines.
func listChats(ctx context.Context, acc accountSurface, out io.Writer, asJSON bool) error {
	all, err := acc.Chats(ctx)
	if err != nil {
		return err
	}
	var rows []project.ChatRow
	for _, c := range all {
		if !c.Archived {
			rows = append(rows, c)
		}
	}
	if asJSON {
		return chatsJSON(out, rows)
	}
	_, err = io.WriteString(out, chatsText(rows))
	return err
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
// A chat with no badges ends at its title, so no line carries trailing spaces.
func chatsText(rows []project.ChatRow) string {
	idW, titleW := 0, 0
	for _, c := range rows {
		idW = max(idW, len(fmt.Sprint(c.ID)))
		titleW = max(titleW, ansi.StringWidth(c.Title))
	}
	var b strings.Builder
	for _, c := range rows {
		id := fmt.Sprint(c.ID)
		b.WriteString(id)
		b.WriteString(strings.Repeat(" ", idW-len(id)+2))
		b.WriteString(c.Title)
		if badges := chatBadges(c); badges != "" {
			b.WriteString(strings.Repeat(" ", titleW-ansi.StringWidth(c.Title)+2))
			b.WriteString(badges)
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
