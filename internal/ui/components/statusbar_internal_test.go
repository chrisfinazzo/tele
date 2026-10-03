package components

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/sorokin-vladimir/tele/internal/ui/keys"
	"github.com/sorokin-vladimir/tele/internal/ui/theme"
	"github.com/stretchr/testify/assert"
)

func TestVersionLabel(t *testing.T) {
	assert.Equal(t, "", versionLabel(""))
	assert.Equal(t, "dev", versionLabel("dev"))
	assert.Equal(t, "v1.2.3", versionLabel("1.2.3"))
	assert.Equal(t, "v1.2.3", versionLabel("v1.2.3"))
}

// Both sizes are written in the unit of the whole, with a decimal only where a
// whole number would be too coarse to see move (#204).
func TestTransferProgress(t *testing.T) {
	const kb, mb, gb = int64(1) << 10, int64(1) << 20, int64(1) << 30
	cases := []struct {
		done, total int64
		want        string
	}{
		{168 * mb, 400 * mb, "42% · 168/400 MB"},
		{gb + gb/5, 3*gb + 2*gb/5, "35% · 1.2/3.4 GB"},
		{3 * mb / 10, 2*mb + mb/10, "14% · 0.3/2.1 MB"},
		{200 * kb, 800 * kb, "25% · 200/800 KB"},
		{300, 900, "33% · 300/900 B"},
		{0, 400 * mb, "0% · 0/400 MB"},
		{410 * mb, 400 * mb, "100% · 410/400 MB"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, transferProgress(c.done, c.total))
	}
}

func TestOverlayHint_JoinsPairs(t *testing.T) {
	out := OverlayHint([][2]string{{"space", "pause"}, {"q", "close"}}, nil)
	if !strings.Contains(out, "pause") || !strings.Contains(out, "close") {
		t.Fatalf("overlay hint missing entries: %q", out)
	}
}

func TestHintLayout_LetterInWord_HighlightsInPlace(t *testing.T) {
	text, spans := hintLayout("q", "quit")
	assert.Equal(t, "quit", text)
	assert.Equal(t, []span{{0, 1}}, spans)
}

func TestHintLayout_LetterInWord_MidWord(t *testing.T) {
	// "a" is at rune index 1 of "caption".
	text, spans := hintLayout("a", "caption")
	assert.Equal(t, "caption", text)
	assert.Equal(t, []span{{1, 2}}, spans)
}

func TestHintLayout_LetterInWord_CaseInsensitive(t *testing.T) {
	// A lowercase key matches the capital letter but is shown in the key's case,
	// so the highlighted letter reads as the actual keystroke.
	text, spans := hintLayout("n", "Nice")
	assert.Equal(t, "nice", text)
	assert.Equal(t, []span{{0, 1}}, spans)
}

func TestHintLayout_LetterNotInWord_PrefixForm(t *testing.T) {
	// "attach" has no "f".
	text, spans := hintLayout("f", "attach")
	assert.Equal(t, "f attach", text)
	assert.Equal(t, []span{{0, 1}}, spans)
}

func TestHintLayout_UppercaseKey_HighlightsCapitalInPlace(t *testing.T) {
	// A Shift key matches the word's capital letter and is highlighted in place;
	// the capital already conveys that Shift is required.
	text, spans := hintLayout("O", "Open photo externally")
	assert.Equal(t, "Open photo externally", text)
	assert.Equal(t, []span{{0, 1}}, spans)
}

func TestHintLayout_NonLetterKey_PrefixForm(t *testing.T) {
	text, spans := hintLayout("/", "search")
	assert.Equal(t, "/ search", text)
	assert.Equal(t, []span{{0, 1}}, spans)
}

func TestHintLayout_NamedKey_PrefixForm(t *testing.T) {
	text, spans := hintLayout("esc", "cancel")
	assert.Equal(t, "esc cancel", text)
	assert.Equal(t, []span{{0, 3}}, spans)
}

func TestHintLayout_ComboKey_PrefixForm(t *testing.T) {
	text, spans := hintLayout("ctrl+t", "photo/file")
	assert.Equal(t, "ctrl+t photo/file", text)
	assert.Equal(t, []span{{0, 6}}, spans)
}

func TestHintLayout_Enter_SuffixGlyph(t *testing.T) {
	text, spans := hintLayout("enter", "send")
	assert.Equal(t, "send ↵", text)
	// "send " is 5 runes; the glyph is the 6th rune.
	assert.Equal(t, []span{{5, 6}}, spans)
}

func TestHintLayout_EmptyKey_NoAccent(t *testing.T) {
	text, spans := hintLayout("", "quit")
	assert.Equal(t, "quit", text)
	assert.Nil(t, spans)
}

func TestNavLayout_NonArrowPair_PrefixForm(t *testing.T) {
	text, spans := navLayout("j", "k", "move")
	assert.Equal(t, "j/k move", text)
	assert.Equal(t, []span{{0, 3}}, spans)
}

func TestNavLayout_SharedModifierCollapsed(t *testing.T) {
	text, spans := navLayout("ctrl+j", "ctrl+k", "select")
	assert.Equal(t, "ctrl+j/k select", text)
	assert.Equal(t, []span{{0, 8}}, spans)
}

func TestNavLayout_ArrowPair_GlyphForm(t *testing.T) {
	text, spans := navLayout("down", "up", "select")
	assert.Equal(t, "↑ select ↓", text)
	// 10 runes total; accent the first and last.
	assert.Equal(t, []span{{0, 1}, {9, 10}}, spans)
}

func TestNavLayout_EmptyPair_Empty(t *testing.T) {
	text, spans := navLayout("", "", "move")
	assert.Equal(t, "", text)
	assert.Nil(t, spans)
}

func TestApplyAccent_WrapsOnlySpans(t *testing.T) {
	accent := lipgloss.NewStyle().Foreground(theme.T().Accent)
	out := applyAccent("quit", []span{{0, 1}}, theme.S().Bar, accent)
	// The rune "q" is styled, "uit" is left as-is.
	assert.Contains(t, out, "uit")
	assert.NotEqual(t, "quit", out) // styling was applied
}

func TestApplyAccent_NoSpans_UsesBaseStyle(t *testing.T) {
	accent := lipgloss.NewStyle().Foreground(theme.T().Accent)
	// With no accent, the whole text is rendered with the base style.
	assert.Equal(t, theme.S().Bar.Render("plain"), applyAccent("plain", nil, theme.S().Bar, accent))
}

func TestApplyAccent_NonAccentRunUsesBaseStyle(t *testing.T) {
	accent := lipgloss.NewStyle().Background(theme.T().SurfaceStatusBar).Foreground(theme.T().Accent)
	out := applyAccent("quit", []span{{0, 1}}, theme.S().Bar, accent)
	// The non-accent remainder must be styled with the base (bar) style, not
	// left plain (which would lose the background after the accent's reset).
	assert.Contains(t, out, theme.S().Bar.Render("uit"))
}

func TestJoinHints_SeparatorKeepsBarBackground(t *testing.T) {
	out := joinHints("a", "b")
	assert.Contains(t, out, theme.S().Bar.Render(" · "))
}

func TestStatusBar_VersionFillerAndTextKeepBarStyle(t *testing.T) {
	sb := NewStatusBar(80)
	sb.SetMode(keys.ModeNormal)
	sb.SetVersion("1.2.3")
	out := sb.View()

	// The filler between the segments and the version must carry the bar
	// background itself: the preceding run ends with a reset, so plain spaces
	// would show the terminal background instead.
	gap := 80 - lipgloss.Width(theme.S().ModeNormal.Render("NORMAL")) - lipgloss.Width("v1.2.3")
	assert.Contains(t, out, theme.S().Bar.Render(strings.Repeat(" ", gap)))
	// The version reads in the bar's normal text color, like a hint's wording.
	assert.Contains(t, out, theme.S().Bar.Render("v1.2.3"))
}

func TestAccentStyle_FollowsMode(t *testing.T) {
	sb := NewStatusBar(80)
	sb.SetMode(keys.ModeNormal)
	normal := sb.accentStyle()
	sb.SetMode(keys.ModeInsert)
	insert := sb.accentStyle()
	// Different foreground per mode; we assert the rendered escapes differ.
	assert.NotEqual(t, normal.Render("x"), insert.Render("x"))
}
