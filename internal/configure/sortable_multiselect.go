package configure

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SortColumn identifies a sort dimension for the sortable multi-select.
type SortColumn int

const (
	SortByName     SortColumn = iota
	SortByCreated             // sort by DiscoverableOption.Created
	SortByModified            // sort by DiscoverableOption.Updated
)

// SortableMultiSelect is a bubbletea Model that renders a scrollable, sortable,
// multi-select list with k9s-style sort hotkeys and a legend footer.
type SortableMultiSelect struct {
	title       string
	description string
	options     []DiscoverableOption // master list (selection state lives here)
	sorted      []int                // index permutation in current display order
	cursor      int                  // cursor row within sorted
	offset      int                  // viewport scroll offset
	height      int                  // number of visible item rows
	width       int
	sortCol     SortColumn
	ascending   bool
	selected    map[string]bool // option.ID -> toggled
	done        bool
	aborted     bool
}

// NewSortableMultiSelect creates a SortableMultiSelect pre-populated with options.
// Default sort is by name ascending.
func NewSortableMultiSelect(title, description string, options []DiscoverableOption) SortableMultiSelect {
	selected := make(map[string]bool, len(options))
	for _, o := range options {
		if o.Selected {
			selected[o.ID] = true
		}
	}

	m := SortableMultiSelect{
		title:       title,
		description: description,
		options:     options,
		sortCol:     SortByName,
		ascending:   true,
		selected:    selected,
		height:      20, // sensible default; overridden by WindowSizeMsg
		width:       80,
	}

	m.buildSorted()

	return m
}

// SelectedIDs returns the IDs of all currently selected options in original order.
func (m SortableMultiSelect) SelectedIDs() []string {
	ids := make([]string, 0, len(m.selected))
	for _, o := range m.options {
		if m.selected[o.ID] {
			ids = append(ids, o.ID)
		}
	}

	return ids
}

// Aborted reports whether the user dismissed without confirming.
func (m SortableMultiSelect) Aborted() bool {
	return m.aborted
}

func (m *SortableMultiSelect) buildSorted() {
	indices := make([]int, len(m.options))
	for i := range indices {
		indices[i] = i
	}

	col := m.sortCol
	asc := m.ascending

	slices.SortStableFunc(indices, func(a, b int) int {
		oa, ob := m.options[a], m.options[b]

		var cmp int

		switch col {
		case SortByCreated:
			ta, tb := oa.Created, ob.Created
			switch {
			case ta.IsZero() && tb.IsZero():
				cmp = 0
			case ta.IsZero():
				return 1 // zero dates sort to end regardless of direction
			case tb.IsZero():
				return -1
			default:
				cmp = ta.Compare(tb)
			}
		case SortByModified:
			ta, tb := oa.Updated, ob.Updated
			switch {
			case ta.IsZero() && tb.IsZero():
				cmp = 0
			case ta.IsZero():
				return 1
			case tb.IsZero():
				return -1
			default:
				cmp = ta.Compare(tb)
			}
		default: // SortByName
			cmp = strings.Compare(strings.ToLower(oa.Name), strings.ToLower(ob.Name))
		}

		if !asc {
			cmp = -cmp
		}

		return cmp
	})

	m.sorted = indices
}

func (m SortableMultiSelect) Init() tea.Cmd {
	return nil
}

func (m SortableMultiSelect) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		// Reserve rows: title(1) + description(1) + blank(1) + header(1) + blank(1) + legend(1) + blank(1) = 7
		m.height = max(5, msg.Height-7)

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			m.aborted = true
			m.done = true

		case "enter":
			m.done = true

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				if m.cursor < m.offset {
					m.offset = m.cursor
				}
			}

		case "down", "j":
			if m.cursor < len(m.sorted)-1 {
				m.cursor++
				if m.cursor >= m.offset+m.height {
					m.offset = m.cursor - m.height + 1
				}
			}

		case " ", "x":
			if len(m.sorted) > 0 {
				id := m.options[m.sorted[m.cursor]].ID
				m.selected[id] = !m.selected[id]
			}

		case "a":
			for _, o := range m.options {
				m.selected[o.ID] = true
			}

		case "n":
			for k := range m.selected {
				m.selected[k] = false
			}

		case "N": // Shift-N: sort by name
			if m.sortCol == SortByName {
				m.ascending = !m.ascending
			} else {
				m.sortCol = SortByName
				m.ascending = true
			}

			m.buildSorted()
			m.cursor = 0
			m.offset = 0

		case "C": // Shift-C: sort by created
			if m.sortCol == SortByCreated {
				m.ascending = !m.ascending
			} else {
				m.sortCol = SortByCreated
				m.ascending = false // most-recently-created first by default
			}

			m.buildSorted()
			m.cursor = 0
			m.offset = 0

		case "M": // Shift-M: sort by modified
			if m.sortCol == SortByModified {
				m.ascending = !m.ascending
			} else {
				m.sortCol = SortByModified
				m.ascending = false // most-recently-modified first by default
			}

			m.buildSorted()
			m.cursor = 0
			m.offset = 0
		}
	}

	if m.done {
		return m, tea.Quit
	}

	return m, nil
}

// column widths for the table layout.
const (
	colDateWidth = 12 // "2006-01-02" + 2 padding
	minWidthFull = 60 // below this width, date columns are hidden
)

func (m SortableMultiSelect) View() string {
	showDates := m.width >= minWidthFull

	var sb strings.Builder

	// Title + description.
	sb.WriteString(titleStyle.Render(m.title))
	sb.WriteByte('\n')
	sb.WriteString(descStyle.Render(m.description))
	sb.WriteByte('\n')
	sb.WriteByte('\n')

	// Header row.
	sb.WriteString(m.renderHeader(showDates))
	sb.WriteByte('\n')
	sb.WriteByte('\n')

	// Item rows.
	end := min(m.offset+m.height, len(m.sorted))

	for row := m.offset; row < end; row++ {
		idx := m.sorted[row]
		opt := m.options[idx]
		isCursor := row == m.cursor
		isSelected := m.selected[opt.ID]
		sb.WriteString(m.renderRow(opt, isCursor, isSelected, showDates))
		sb.WriteByte('\n')
	}

	// Scroll indicator.
	if len(m.sorted) > m.height {
		shown := min(m.offset+m.height, len(m.sorted))
		sb.WriteString(dimStyle.Render(fmt.Sprintf("  %d–%d of %d", m.offset+1, shown, len(m.sorted))))
		sb.WriteByte('\n')
	}

	sb.WriteByte('\n')
	sb.WriteString(m.renderLegend())

	return sb.String()
}

func (m SortableMultiSelect) renderHeader(showDates bool) string {
	arrow := m.sortArrow()

	nameHeader := "Name"
	createdHeader := "Created"
	modifiedHeader := "Modified"

	switch m.sortCol {
	case SortByName:
		nameHeader = arrow + nameHeader
	case SortByCreated:
		createdHeader = arrow + createdHeader
	case SortByModified:
		modifiedHeader = arrow + modifiedHeader
	}

	nameWidth := m.nameColWidth(showDates)
	// Pad plain text BEFORE applying lipgloss — rendering adds ANSI codes that
	// padRight would count as runes, breaking column alignment.
	header := "      " + headerStyle.Render(padRight(nameHeader, nameWidth))

	if showDates {
		header += "  " + headerStyle.Render(padRight(createdHeader, colDateWidth))
		header += "  " + headerStyle.Render(modifiedHeader)
	}

	return header
}

func (m SortableMultiSelect) renderRow(opt DiscoverableOption, isCursor, isSelected, showDates bool) string {
	checkbox := "[ ]"
	if isSelected {
		checkbox = selectedStyle.Render("[x]")
	}

	nameWidth := m.nameColWidth(showDates)
	name := padRight(opt.Name, nameWidth)

	if isCursor {
		name = cursorStyle.Render(name)
	}

	line := fmt.Sprintf("  %s %s", checkbox, name)

	if showDates {
		created := dimStyle.Render(padRight(FormatCompactDate(opt.Created), colDateWidth))
		updated := dimStyle.Render(FormatCompactDate(opt.Updated))
		line += fmt.Sprintf("  %s  %s", created, updated)
	}

	return line
}

func (m SortableMultiSelect) renderLegend() string {
	// Sort hotkeys.
	hotkeys := []string{
		hotkey("N", "ame"),
		hotkey("C", "reated"),
		hotkey("M", "odified"),
	}

	// Current sort state indicator.
	colName := map[SortColumn]string{
		SortByName:     "Name",
		SortByCreated:  "Created",
		SortByModified: "Modified",
	}[m.sortCol]

	sortIndicator := dimStyle.Render(m.sortArrow() + colName)

	// Action hints.
	actions := dimStyle.Render("space:toggle  a:all  n:none  enter:confirm  esc:cancel")

	sep := "  " + dimSepStyle.Render("│") + "  "

	return strings.Join(hotkeys, "  ") + sep + sortIndicator + sep + actions
}

func (m SortableMultiSelect) sortArrow() string {
	if m.ascending {
		return "↑"
	}

	return "↓"
}

func (m SortableMultiSelect) nameColWidth(showDates bool) int {
	if !showDates {
		return m.width - 6 // checkbox(3) + spaces(2) + margin(1)
	}
	// width - checkbox(3) - spaces(2) - 2*date cols - separators
	w := m.width - 3 - 2 - 2*(colDateWidth+2)
	if w < 20 {
		w = 20
	}

	return w
}

// padRight pads or truncates s to exactly n runes.
func padRight(s string, n int) string {
	runes := []rune(s)
	if len(runes) >= n {
		return string(runes[:n])
	}

	return s + strings.Repeat(" ", n-len(runes))
}

// hotkey renders a k9s-style "<X>rest" legend entry where X is highlighted.
func hotkey(letter, rest string) string {
	return hotkeyStyle.Render("<"+letter+">") + dimStyle.Render(rest)
}

// Styles.
var (
	titleStyle    = lipgloss.NewStyle().Bold(true)
	descStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	headerStyle   = lipgloss.NewStyle().Bold(true).Underline(true)
	cursorStyle   = lipgloss.NewStyle().Background(lipgloss.Color("236"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	dimSepStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	hotkeyStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
)
