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
	SortByOwner               // sort by DiscoverableOption.Owner
)

// SortableMultiSelect is a bubbletea Model that renders a scrollable, sortable,
// multi-select list with sort hotkeys, a legend footer, and a live filter.
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
	filter      string          // current filter string (empty = no filter)
	filtering   bool            // true while the user is typing a filter
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
	filterLow := strings.ToLower(m.filter)

	indices := make([]int, 0, len(m.options))
	for i, o := range m.options {
		if filterLow == "" || strings.Contains(strings.ToLower(o.Name), filterLow) ||
			strings.Contains(strings.ToLower(o.Owner), filterLow) {
			indices = append(indices, i)
		}
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
		case SortByOwner:
			cmp = strings.Compare(strings.ToLower(oa.Owner), strings.ToLower(ob.Owner))
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
		// While filtering, route keys to the filter input.
		if m.filtering {
			switch msg.String() {
			case "enter", "esc":
				m.filtering = false
			case "ctrl+c":
				m.filter = ""
				m.filtering = false
				m.buildSorted()
				m.cursor = 0
				m.offset = 0
			case "backspace":
				if len(m.filter) > 0 {
					m.filter = m.filter[:len(m.filter)-1]
					m.buildSorted()
					m.cursor = 0
					m.offset = 0
				}
			default:
				// Append printable runes to the filter.
				if len(msg.Runes) > 0 {
					m.filter += string(msg.Runes)
					m.buildSorted()
					m.cursor = 0
					m.offset = 0
				}
			}

			break
		}

		switch msg.String() {
		case "ctrl+c":
			m.aborted = true
			m.done = true

		case "esc", "q":
			// If a filter is active, esc clears it first rather than aborting.
			if m.filter != "" {
				m.filter = ""
				m.buildSorted()
				m.cursor = 0
				m.offset = 0
			} else {
				m.aborted = true
				m.done = true
			}

		case "enter":
			m.done = true

		case "/":
			m.filtering = true

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

		case "pgup", "ctrl+b":
			m.cursor = max(0, m.cursor-m.height)
			m.offset = max(0, m.offset-m.height)

		case "pgdown", "ctrl+f":
			last := len(m.sorted) - 1
			m.cursor = min(last, m.cursor+m.height)

			if m.cursor >= m.offset+m.height {
				m.offset = m.cursor - m.height + 1
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

		case "O": // Shift-O: sort by owner
			if m.sortCol == SortByOwner {
				m.ascending = !m.ascending
			} else {
				m.sortCol = SortByOwner
				m.ascending = true
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
	colDateWidth  = 12 // "2006-01-02" + 2 padding
	colOwnerWidth = 22 // owner display name, truncated
	minWidthFull  = 60 // below this width, date columns are hidden
	minWidthOwner = 90 // below this width, owner column is hidden
)

// hasOwners reports whether any option has a non-empty Owner field.
func (m SortableMultiSelect) hasOwners() bool {
	for _, o := range m.options {
		if o.Owner != "" {
			return true
		}
	}

	return false
}

func (m SortableMultiSelect) View() string {
	showDates := m.width >= minWidthFull
	showOwner := m.width >= minWidthOwner && m.hasOwners()

	var sb strings.Builder

	// Title + description.
	sb.WriteString(titleStyle.Render(m.title))
	sb.WriteByte('\n')
	sb.WriteString(descStyle.Render(m.description))
	sb.WriteByte('\n')
	sb.WriteByte('\n')

	// Filter bar — shown when actively filtering or when a filter is set.
	if m.filtering || m.filter != "" {
		cursor := ""
		if m.filtering {
			cursor = "█"
		}

		sb.WriteString(filterStyle.Render(fmt.Sprintf("  Filter: %s%s", m.filter, cursor)))
		sb.WriteByte('\n')
		sb.WriteByte('\n')
	}

	// Header row.
	sb.WriteString(m.renderHeader(showDates, showOwner))
	sb.WriteByte('\n')
	sb.WriteByte('\n')

	// Item rows.
	end := min(m.offset+m.height, len(m.sorted))

	for row := m.offset; row < end; row++ {
		idx := m.sorted[row]
		opt := m.options[idx]
		isCursor := row == m.cursor
		isSelected := m.selected[opt.ID]
		sb.WriteString(m.renderRow(opt, isCursor, isSelected, showDates, showOwner))
		sb.WriteByte('\n')
	}

	// Scroll indicator.
	if len(m.sorted) > m.height {
		shown := min(m.offset+m.height, len(m.sorted))
		total := len(m.options)

		if m.filter != "" {
			indicator := fmt.Sprintf("  %d–%d of %d (filtered from %d)", m.offset+1, shown, len(m.sorted), total)
			sb.WriteString(dimStyle.Render(indicator))
		} else {
			sb.WriteString(dimStyle.Render(fmt.Sprintf("  %d–%d of %d", m.offset+1, shown, total)))
		}

		sb.WriteByte('\n')
	}

	sb.WriteByte('\n')
	sb.WriteString(m.renderLegend())

	return sb.String()
}

func (m SortableMultiSelect) renderHeader(showDates, showOwner bool) string {
	arrow := m.sortArrow()

	nameHeader := "Name"
	ownerHeader := "Owner"
	createdHeader := "Created"
	modifiedHeader := "Modified"

	switch m.sortCol {
	case SortByName:
		nameHeader = arrow + nameHeader
	case SortByOwner:
		ownerHeader = arrow + ownerHeader
	case SortByCreated:
		createdHeader = arrow + createdHeader
	case SortByModified:
		modifiedHeader = arrow + modifiedHeader
	}

	nameWidth := m.nameColWidth(showDates, showOwner)
	// Pad plain text BEFORE applying lipgloss — rendering adds ANSI codes that
	// padRight would count as runes, breaking column alignment.
	header := "      " + headerStyle.Render(padRight(nameHeader, nameWidth))

	if showOwner {
		header += "  " + headerStyle.Render(padRight(ownerHeader, colOwnerWidth))
	}

	if showDates {
		header += "  " + headerStyle.Render(padRight(createdHeader, colDateWidth))
		header += "  " + headerStyle.Render(modifiedHeader)
	}

	return header
}

func (m SortableMultiSelect) renderRow(opt DiscoverableOption, isCursor, isSelected, showDates, showOwner bool) string {
	checkbox := "[ ]"
	if isSelected {
		checkbox = selectedStyle.Render("[x]")
	}

	nameWidth := m.nameColWidth(showDates, showOwner)
	name := padRight(opt.Name, nameWidth)

	if isCursor {
		name = cursorStyle.Render(name)
	}

	line := fmt.Sprintf("  %s %s", checkbox, name)

	if showOwner {
		line += "  " + dimStyle.Render(padRight(opt.Owner, colOwnerWidth))
	}

	if showDates {
		created := dimStyle.Render(padRight(FormatCompactDate(opt.Created), colDateWidth))
		updated := dimStyle.Render(FormatCompactDate(opt.Updated))
		line += fmt.Sprintf("  %s  %s", created, updated)
	}

	return line
}

func (m SortableMultiSelect) renderLegend() string {
	// Sort hotkeys — show <O>wner only when owner data is present.
	hotkeys := []string{
		hotkey("N", "ame"),
		hotkey("C", "reated"),
		hotkey("M", "odified"),
	}

	if m.hasOwners() {
		hotkeys = append(hotkeys, hotkey("O", "wner"))
	}

	// Current sort state indicator.
	colName := map[SortColumn]string{
		SortByName:     "Name",
		SortByCreated:  "Created",
		SortByModified: "Modified",
		SortByOwner:    "Owner",
	}[m.sortCol]

	sortIndicator := dimStyle.Render(m.sortArrow() + colName)

	// Action hints.
	actions := dimStyle.Render("space:toggle  a:all  n:none  /:filter  pgup/pgdn:page  enter:confirm  esc:cancel")

	sep := "  " + dimSepStyle.Render("│") + "  "

	return strings.Join(hotkeys, "  ") + sep + sortIndicator + sep + actions
}

func (m SortableMultiSelect) sortArrow() string {
	if m.ascending {
		return "↑"
	}

	return "↓"
}

func (m SortableMultiSelect) nameColWidth(showDates, showOwner bool) int {
	if !showDates {
		return m.width - 6 // checkbox(3) + spaces(2) + margin(1)
	}
	// width - checkbox(3) - spaces(2) - 2*date cols - separators
	w := m.width - 3 - 2 - 2*(colDateWidth+2)
	if showOwner {
		w -= colOwnerWidth + 2
	}

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
	filterStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
)
