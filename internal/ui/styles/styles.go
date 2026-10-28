package styles

import "github.com/charmbracelet/lipgloss"

// Palette ---------------------------------------------------------------------
//
// Semantic roles. The app only references these — no ad-hoc colors anywhere
// else. The palette targets a dark terminal; adaptive variants can be layered
// in later without changing call sites.
var (
	// Core surfaces (dark → light).
	ColorBg      = lipgloss.Color("#13111C") // app background (true black-purple)
	ColorSurface = lipgloss.Color("#1C1A2A") // list rows, panels
	ColorOverlay = lipgloss.Color("#262338") // header rail, info bar
	ColorRaised  = lipgloss.Color("#2F2B45") // selected row, chips
	ColorBorder  = lipgloss.Color("#3A3550") // thin dividers
	ColorMuted   = lipgloss.Color("#2A2A3C") // status bar background (legacy)

	// Brand / accent.
	ColorPrimary   = lipgloss.Color("#9B7CFF") // iris — primary brand
	ColorPrimaryHi = lipgloss.Color("#B8A1FF") // lighter brand (for highlights)
	ColorAccent    = lipgloss.Color("#73F59F") // mint — unread, success
	ColorAccentDim = lipgloss.Color("#4FB97A") // deeper mint (for faint dot)
	ColorHeaderSep = lipgloss.Color("#9E80FF") // header separator

	// Text.
	ColorFg       = lipgloss.Color("#F4F2FF") // primary text
	ColorFgStrong = lipgloss.Color("#FFFFFF") // emphasized text
	ColorSubdued  = lipgloss.Color("#C7C2E0") // secondary text
	ColorDimFg    = lipgloss.Color("#716C8F") // tertiary / meta
	ColorFaint    = lipgloss.Color("#4B4668") // faint / borders text

	// Semantic.
	ColorWarn   = lipgloss.Color("#F5C06E") // amber
	ColorDanger = lipgloss.Color("#FF6E8A") // rose
	ColorInfo   = lipgloss.Color("#7EC8FF") // sky
)

// Header ----------------------------------------------------------------------
//
// Two-row header: a bold brand bar on top, a context strip beneath. All
// segments share the row background so they tile seamlessly.
var (
	HeaderBar = lipgloss.NewStyle().
			Background(ColorOverlay).
			Foreground(ColorFg)

	HeaderAppName = lipgloss.NewStyle().
			Background(ColorPrimary).
			Foreground(ColorFgStrong).
			Bold(true).
			Padding(0, 1)

	HeaderAppTag = lipgloss.NewStyle().
			Background(ColorOverlay).
			Foreground(ColorPrimaryHi).
			Italic(true)

	HeaderAccount = lipgloss.NewStyle().
			Background(ColorOverlay).
			Foreground(ColorFg).
			Bold(true)

	HeaderAccountIcon = lipgloss.NewStyle().
				Background(ColorOverlay).
				Foreground(ColorAccent)

	HeaderSep = lipgloss.NewStyle().
			Background(ColorOverlay).
			Foreground(ColorBorder)

	HeaderBreadcrumb = lipgloss.NewStyle().
				Background(ColorOverlay).
				Foreground(ColorSubdued)

	HeaderBreadcrumbCur = lipgloss.NewStyle().
				Background(ColorOverlay).
				Foreground(ColorFgStrong).
				Bold(true)

	HeaderMeta = lipgloss.NewStyle().
			Background(ColorOverlay).
			Foreground(ColorDimFg)

	HeaderMetaValue = lipgloss.NewStyle().
			Background(ColorOverlay).
			Foreground(ColorSubdued)

	HeaderAccent = lipgloss.NewStyle().
			Background(ColorOverlay).
			Foreground(ColorAccent)

	HeaderRule = lipgloss.NewStyle().
			Background(ColorBg).
			Foreground(ColorBorder)
)

// List column header + count line -------------------------------------------
var (
	ListColHeader = lipgloss.NewStyle().
			Background(ColorBg).
			Foreground(ColorDimFg).
			Bold(true)

	ListCount = lipgloss.NewStyle().
			Background(ColorBg).
			Foreground(ColorSubdued)

	ListCountAccent = lipgloss.NewStyle().
			Background(ColorBg).
			Foreground(ColorAccent).
			Bold(true)

	ListFolder = lipgloss.NewStyle().
			Background(ColorBg).
			Foreground(ColorPrimaryHi).
			Bold(true)
)

// Mail list item styles -------------------------------------------------------
var (
	ItemBg = lipgloss.NewStyle().Background(ColorBg)

	ItemBgSelected = lipgloss.NewStyle().Background(ColorRaised)

	// Left-edge selection bar.
	SelectionBar = lipgloss.NewStyle().
			Foreground(ColorPrimaryHi).
			Background(ColorRaised).
			Bold(true)

	SelectionBarIdle = lipgloss.NewStyle().Background(ColorBg)

	// Unread marker dot.
	ItemUnreadDot = lipgloss.NewStyle().
			Foreground(ColorAccent).
			Bold(true)

	ItemUnreadDotRead = lipgloss.NewStyle().
				Foreground(ColorFaint)

	// Sender.
	ItemFrom = lipgloss.NewStyle().Foreground(ColorSubdued)

	ItemFromUnread = lipgloss.NewStyle().
			Foreground(ColorFgStrong).
			Bold(true)

	// Subject.
	ItemSubject = lipgloss.NewStyle().Foreground(ColorDimFg)

	ItemSubjectUnread = lipgloss.NewStyle().
				Foreground(ColorFg).
				Bold(true)

	// Preview.
	ItemPreview = lipgloss.NewStyle().Foreground(ColorFaint).Italic(true)

	// Date.
	ItemDate = lipgloss.NewStyle().Foreground(ColorDimFg)

	ItemDateUnread = lipgloss.NewStyle().
			Foreground(ColorPrimaryHi).
			Bold(true)

	// Tag chips (attachments, folders, flags).
	TagAttach = lipgloss.NewStyle().
			Foreground(ColorWarn).
			Bold(true)

	TagFlag = lipgloss.NewStyle().
		Foreground(ColorDanger).
		Bold(true)
)

// Mail read view --------------------------------------------------------------
var (
	InfoBar = lipgloss.NewStyle().
		Background(ColorOverlay).
		Foreground(ColorFg)

	InfoLabel = lipgloss.NewStyle().
			Background(ColorOverlay).
			Foreground(ColorDimFg).
			Bold(true)

	InfoValue = lipgloss.NewStyle().
			Background(ColorOverlay).
			Foreground(ColorFg)

	InfoSubject = lipgloss.NewStyle().
			Background(ColorOverlay).
			Foreground(ColorFgStrong).
			Bold(true)

	InfoAccent = lipgloss.NewStyle().
			Background(ColorOverlay).
			Foreground(ColorAccent)

	InfoMeta = lipgloss.NewStyle().
			Background(ColorOverlay).
			Foreground(ColorDimFg)

	InfoBodyKind = lipgloss.NewStyle().
			Background(ColorBg).
			Foreground(ColorInfo)

	InfoRule = lipgloss.NewStyle().
			Background(ColorBg).
			Foreground(ColorBorder)
)

// Status bar ------------------------------------------------------------------
var (
	StatusBar = lipgloss.NewStyle().
			Background(ColorOverlay).
			Foreground(ColorSubdued)

	StatusKey = lipgloss.NewStyle().
			Background(ColorRaised).
			Foreground(ColorPrimaryHi).
			Bold(true).
			Padding(0, 1)

	StatusKeyLabel = lipgloss.NewStyle().
			Background(ColorOverlay).
			Foreground(ColorSubdued)

	StatusSep = lipgloss.NewStyle().
			Background(ColorOverlay).
			Foreground(ColorBorder)

	StatusMode = lipgloss.NewStyle().
			Background(ColorPrimary).
			Foreground(ColorFgStrong).
			Bold(true).
			Padding(0, 1)

	StatusModeLeader = lipgloss.NewStyle().
				Background(ColorAccent).
				Foreground(ColorBg).
				Bold(true).
				Padding(0, 1)
)

// Whichkey popup -------------------------------------------------------------
//
// A floating leader-key popup rendered at the bottom of the screen. Painted
// on ColorOverlay so it reads as a distinct raised panel, bounded above and
// below by thin accented rules.
var (
	WhichKeyPanel = lipgloss.NewStyle().
			Background(ColorOverlay).
			Foreground(ColorFg)

	WhichKeyRuleTop = lipgloss.NewStyle().
			Background(ColorBg).
			Foreground(ColorPrimary).
			Bold(true)

	WhichKeyRuleMid = lipgloss.NewStyle().
			Background(ColorOverlay).
			Foreground(ColorBorder)

	WhichKeyRuleBot = lipgloss.NewStyle().
			Background(ColorBg).
			Foreground(ColorBorder)

	// Title row ---------------------------------------------------------
	WhichKeyLeader = lipgloss.NewStyle().
			Background(ColorPrimary).
			Foreground(ColorFgStrong).
			Bold(true)

	WhichKeyPathKey = lipgloss.NewStyle().
			Background(ColorRaised).
			Foreground(ColorPrimaryHi).
			Bold(true)

	WhichKeyPathSep = lipgloss.NewStyle().
			Background(ColorOverlay).
			Foreground(ColorBorder)

	WhichKeyGroupLabel = lipgloss.NewStyle().
				Background(ColorOverlay).
				Foreground(ColorAccent).
				Italic(true).
				Bold(true)

	WhichKeyCount = lipgloss.NewStyle().
			Background(ColorOverlay).
			Foreground(ColorDimFg)

	WhichKeyCancelKey = lipgloss.NewStyle().
				Background(ColorRaised).
				Foreground(ColorDanger).
				Bold(true)

	WhichKeyCancelLabel = lipgloss.NewStyle().
				Background(ColorOverlay).
				Foreground(ColorSubdued)

	// Chip ---------------------------------------------------------------
	WhichKeyChipKey = lipgloss.NewStyle().
			Background(ColorRaised).
			Foreground(ColorPrimaryHi).
			Bold(true)

	WhichKeyChipDesc = lipgloss.NewStyle().
				Background(ColorOverlay).
				Foreground(ColorFg)

	WhichKeyChipKeyGroup = lipgloss.NewStyle().
				Background(ColorRaised).
				Foreground(ColorAccent).
				Bold(true)

	WhichKeyChipDescGroup = lipgloss.NewStyle().
				Background(ColorOverlay).
				Foreground(ColorAccent).
				Bold(true)

	WhichKeyChipKeyAccent = lipgloss.NewStyle().
				Background(ColorRaised).
				Foreground(ColorAccent).
				Bold(true)

	WhichKeyChipDescAccent = lipgloss.NewStyle().
				Background(ColorOverlay).
				Foreground(ColorAccent)

	WhichKeyChipKeyDanger = lipgloss.NewStyle().
				Background(ColorRaised).
				Foreground(ColorDanger).
				Bold(true)

	WhichKeyChipDescDanger = lipgloss.NewStyle().
				Background(ColorOverlay).
				Foreground(ColorDanger)

	WhichKeyChipArrow = lipgloss.NewStyle().
				Background(ColorOverlay).
				Foreground(ColorDimFg)
)

// Misc / shared ---------------------------------------------------------------
var (
	Unread    = lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	Muted     = lipgloss.NewStyle().Foreground(ColorDimFg)
	Label     = lipgloss.NewStyle().Foreground(ColorDimFg)
	Separator = lipgloss.NewStyle().Foreground(ColorBorder)

	ListTitle = lipgloss.NewStyle().
			Foreground(ColorPrimaryHi).
			Bold(true).
			Padding(0, 1)

	Pane = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, true, false, false).
		BorderForeground(ColorBorder)

	FocusedPane = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, true, false, false).
			BorderForeground(ColorPrimary)
)

// Compose modal --------------------------------------------------------------
//
// The compose flow has three visual stages: a floating form modal, a
// transparent handoff to $EDITOR, and a full-screen preview. All of the
// styles below are shared across those stages so colors tile seamlessly
// with the rest of the app chrome.
var (
	ComposeOverlay = lipgloss.NewStyle().
			Background(ColorBg)

	// NOTE: the ComposePanel / ComposePanelTitle / ComposePanelSubtitle
	// styles that used to live here have been retired. The form modal is
	// now assembled inline in internal/ui/compose.renderForm() as a stack
	// of four full-width sections (title bar, separator, field area,
	// footer) so every row paints edge-to-edge without the "black gaps"
	// the previous shared-padding approach left behind.

	ComposePreviewBar = lipgloss.NewStyle().
				Background(ColorOverlay).
				Foreground(ColorFg)

	ComposeMetaKey = lipgloss.NewStyle().
			Background(ColorOverlay).
			Foreground(ColorDimFg).
			Bold(true)

	ComposeMetaValue = lipgloss.NewStyle().
				Background(ColorOverlay).
				Foreground(ColorFg)

	ComposeMetaSubject = lipgloss.NewStyle().
				Background(ColorOverlay).
				Foreground(ColorFgStrong).
				Bold(true)

	ComposeMetaRule = lipgloss.NewStyle().
			Background(ColorBg).
			Foreground(ColorBorder)

	ComposeMarkdownBadge = lipgloss.NewStyle().
				Background(ColorAccent).
				Foreground(ColorBg).
				Bold(true).
				Padding(0, 1)

	ComposeError = lipgloss.NewStyle().
			Background(ColorBg).
			Foreground(ColorDanger).
			Bold(true)

	ComposeAttachIcon = lipgloss.NewStyle().
				Background(ColorOverlay).
				Foreground(ColorWarn)

	ComposeAttachPillName = lipgloss.NewStyle().
				Background(ColorRaised).
				Foreground(ColorPrimaryHi).
				Bold(true)

	ComposeAttachPillMeta = lipgloss.NewStyle().
				Background(ColorRaised).
				Foreground(ColorDimFg)

	ComposeAttachPillSelected = lipgloss.NewStyle().
					Background(ColorRaised).
					Foreground(ColorAccent).
					Bold(true)

	ComposeAttachMore = lipgloss.NewStyle().
				Background(ColorOverlay).
				Foreground(ColorDimFg)
)

// Account drawer --------------------------------------------------------------
var (
	// AccountActive is the ● filled-circle icon on the currently-active account
	// row. Rendered in ColorAccent bold to match the unread-dot visual language.
	AccountActive = lipgloss.NewStyle().
			Foreground(ColorAccent).
			Bold(true)

	// AccountInactive is the user-icon on non-active rows.
	AccountInactive = lipgloss.NewStyle().
			Foreground(ColorDimFg)

	// AccountName is the account tag on a normal (non-selected, non-active) row.
	AccountName = lipgloss.NewStyle().
			Foreground(ColorSubdued)

	// AccountNameSelected is the account tag on the cursor row.
	AccountNameSelected = lipgloss.NewStyle().
				Foreground(ColorFgStrong).
				Bold(true)

	// AccountNameActive is the account tag for the currently live account
	// (independent of cursor position — the two can differ).
	AccountNameActive = lipgloss.NewStyle().
				Foreground(ColorFg).
				Bold(true)

	// AccountEmail is the second line of an account row (email address).
	AccountEmail = lipgloss.NewStyle().
			Foreground(ColorDimFg)

	// AccountEmailSelected is the email line on the cursor row.
	AccountEmailSelected = lipgloss.NewStyle().
				Foreground(ColorSubdued)

	// AccountBadge is the unread count for values 1–9 (bare numeral, accent).
	AccountBadge = lipgloss.NewStyle().
			Foreground(ColorAccent).
			Bold(true)

	// AccountBadgePill is the background pill for counts 10–99 (and the 99+
	// cap). Inverted to ColorPrimaryHi bg when the row is cursor-selected.
	AccountBadgePill = lipgloss.NewStyle().
				Background(ColorAccent).
				Foreground(ColorBg).
				Bold(true).
				Padding(0, 1)

	// AccountBadgePillSelected is AccountBadgePill with the inverted selection
	// colour (used when the cursor is on the same row as the pill).
	AccountBadgePillSelected = lipgloss.NewStyle().
					Background(ColorPrimaryHi).
					Foreground(ColorBg).
					Bold(true).
					Padding(0, 1)

	// AccountBadgeFaint is the "·" rendered for zero-unread rows.
	AccountBadgeFaint = lipgloss.NewStyle().
				Foreground(ColorFaint)

	// AccountDivider is the thin rule between the title and the account list.
	AccountDivider = lipgloss.NewStyle().
			Background(ColorSurface).
			Foreground(ColorBorder)
)
