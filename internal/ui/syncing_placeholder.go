package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/5Sigma/pmx/internal/ui/styles"
)

// renderSyncingPlaceholder paints the content area with a centred card while
// an account switch is in flight. Pure lipgloss — no spinner, no new
// components. The glyph ⣿ serves as a static activity marker.
func (m Model) renderSyncingPlaceholder() string {
	contentH := m.contentHeight()
	contentW := m.contentWidth()

	// Resolve the active account so we can show name + email.
	var email string
	if m.cfg != nil {
		for _, a := range m.cfg.Accounts {
			if a.Name == m.syncingAcct {
				email = a.Email
				break
			}
		}
	}

	// Card dimensions. Border + 3 content rows = 5 total vertical cells.
	cardWidth := 32
	if max := contentW - 4; max < cardWidth {
		cardWidth = max
	}
	if cardWidth < 12 {
		cardWidth = 12
	}
	const cardHeight = 5
	innerWidth := cardWidth - 2 // minus left/right border cells
	if innerWidth < 1 {
		innerWidth = 1
	}

	// Shared background styles so every cell flushes to ColorBg / ColorSurface
	// — no terminal bleed-through.
	bgStyle := lipgloss.NewStyle().Background(styles.ColorBg)
	cardBg := lipgloss.NewStyle().Background(styles.ColorSurface)

	// Header line inside the card: " switching account " dimmed, centred.
	headerText := " switching account "
	headerStyled := cardBg.Foreground(styles.ColorDimFg).Render(headerText)
	headerLine := lipgloss.PlaceHorizontal(innerWidth, lipgloss.Center,
		headerStyled, lipgloss.WithWhitespaceBackground(styles.ColorSurface))

	// Row 1 (name): "⣿  Connecting <name>"
	glyph := cardBg.Foreground(styles.ColorPrimaryHi).Bold(true).Render("⣿")
	connecting := cardBg.Foreground(styles.ColorSubdued).Render("Connecting ")
	name := cardBg.Foreground(styles.ColorFg).Bold(true).Render(m.syncingAcct)
	nameRow := glyph +
		cardBg.Render("  ") +
		connecting +
		name
	nameLine := lipgloss.PlaceHorizontal(innerWidth, lipgloss.Left,
		nameRow, lipgloss.WithWhitespaceBackground(styles.ColorSurface))

	// Row 2 (email): indented under the name text.
	emailText := email
	if emailText == "" {
		emailText = "—"
	}
	emailRow := cardBg.Render("    ") +
		cardBg.Foreground(styles.ColorDimFg).Render(emailText)
	emailLine := lipgloss.PlaceHorizontal(innerWidth, lipgloss.Left,
		emailRow, lipgloss.WithWhitespaceBackground(styles.ColorSurface))

	inner := lipgloss.JoinVertical(lipgloss.Left,
		headerLine,
		nameLine,
		emailLine,
	)

	card := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorPrimary).
		BorderBackground(styles.ColorBg).
		Background(styles.ColorSurface).
		Width(innerWidth).
		Render(inner)

	// Horizontal centring: pad left/right with ColorBg cells so each row is
	// exactly contentW wide.
	leftPad := (contentW - cardWidth) / 2
	if leftPad < 0 {
		leftPad = 0
	}
	rightPad := contentW - cardWidth - leftPad
	if rightPad < 0 {
		rightPad = 0
	}
	leftFill := bgStyle.Render(strings.Repeat(" ", leftPad))
	rightFill := bgStyle.Render(strings.Repeat(" ", rightPad))

	cardLines := strings.Split(card, "\n")
	for i, line := range cardLines {
		cardLines[i] = leftFill + line + rightFill
	}

	// Vertical centring: fill blank rows above/below so the view is exactly
	// contentH rows tall.
	topPad := (contentH - cardHeight) / 2
	if topPad < 0 {
		topPad = 0
	}
	bottomPad := contentH - cardHeight - topPad
	if bottomPad < 0 {
		bottomPad = 0
	}
	blank := bgStyle.Render(strings.Repeat(" ", contentW))

	rows := make([]string, 0, contentH)
	for i := 0; i < topPad; i++ {
		rows = append(rows, blank)
	}
	rows = append(rows, cardLines...)
	for i := 0; i < bottomPad; i++ {
		rows = append(rows, blank)
	}
	// Guard against size mismatch (e.g. card taller than content area).
	if len(rows) > contentH {
		rows = rows[:contentH]
	}
	for len(rows) < contentH {
		rows = append(rows, blank)
	}

	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}
