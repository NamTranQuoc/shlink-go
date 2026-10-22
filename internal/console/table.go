package console

import (
	"fmt"
	"sort"
	"strings"
)

// TableStyle ports Helper\TableStyle.
type TableStyle struct {
	PaddingChar          string
	HorizontalOutside    string
	HorizontalInside     string
	VerticalOutside      string
	VerticalInside       string
	Crossings            [12]string
	HeaderTitleFormat    string
	FooterTitleFormat    string
	CellHeaderFormat     string
	CellRowFormat        string
	CellRowContentFormat string
	BorderFormat         string
	DisplayOutsideBorder bool
	PadType              int
}

func newTableStyle() *TableStyle {
	s := &TableStyle{
		PaddingChar: " ", HorizontalOutside: "-", HorizontalInside: "-", VerticalOutside: "|", VerticalInside: "|",
		HeaderTitleFormat: "<fg=black;bg=white;options=bold> %s </>", FooterTitleFormat: "<fg=black;bg=white;options=bold> %s </>",
		CellHeaderFormat: "<info>%s</info>", CellRowFormat: "%s", CellRowContentFormat: " %s ", BorderFormat: "%s",
		DisplayOutsideBorder: true, PadType: PadRight,
	}
	s.setDefaultCrossingChar("+")
	return s
}

func (s *TableStyle) setDefaultCrossingChar(c string) {
	for i := range s.Crossings {
		s.Crossings[i] = c
	}
}

// Clone copies a style, like PHP's clone.
func (s *TableStyle) Clone() *TableStyle {
	c := *s
	return &c
}

var tableStyles = initTableStyles()

func initTableStyles() map[string]*TableStyle {
	styleGuide := newTableStyle()
	styleGuide.HorizontalOutside, styleGuide.HorizontalInside = "-", "-"
	styleGuide.VerticalOutside, styleGuide.VerticalInside = " ", " "
	styleGuide.setDefaultCrossingChar(" ")
	styleGuide.CellHeaderFormat = "%s"
	compact := newTableStyle()
	compact.HorizontalOutside, compact.HorizontalInside, compact.VerticalOutside, compact.VerticalInside = "", "", "", ""
	compact.setDefaultCrossingChar("")
	compact.CellRowContentFormat = "%s "
	borderless := newTableStyle()
	borderless.HorizontalOutside, borderless.HorizontalInside = "=", "="
	borderless.VerticalOutside, borderless.VerticalInside = " ", " "
	borderless.setDefaultCrossingChar(" ")
	return map[string]*TableStyle{
		"default": newTableStyle(), "symfony-style-guide": styleGuide, "compact": compact, "borderless": borderless,
	}
}

// TableStyleDefinition ports Table::getStyleDefinition(): the shared, mutable style registered under a name.
func TableStyleDefinition(name string) *TableStyle { return tableStyles[name] }

// TableSeparator ports Helper\TableSeparator.
type TableSeparator struct{}

// TableCell ports Helper\TableCell (colspan only; Shlink uses no rowspans or cell styles).
type TableCell struct {
	Value   string
	Colspan int
}

func (c *TableCell) String() string { return c.Value }

func cellString(c any) string {
	switch v := c.(type) {
	case nil:
		return ""
	case string:
		return v
	case *TableCell:
		return v.Value
	case int:
		return fmt.Sprint(v)
	case int64:
		return fmt.Sprint(v)
	case bool:
		if v {
			return "1"
		}
		return ""
	}
	return fmt.Sprint(c)
}

func colspan(c any) int {
	if tc, ok := c.(*TableCell); ok && tc.Colspan > 1 {
		return tc.Colspan
	}
	return 1
}

// Table ports Helper\Table for the default and horizontal orientations.
type Table struct {
	output       Output
	style        *TableStyle
	headers      [][]any
	rows         []any // []any rows or TableSeparator
	headerTitle  string
	footerTitle  string
	horizontal   bool
	columnWidths map[int]int

	numberOfColumns int
	effectiveWidths []int
}

// NewTable creates a table rendering to an output.
func NewTable(o Output) *Table {
	return &Table{output: o, style: tableStyles["default"], columnWidths: map[int]int{}}
}

func (t *Table) SetStyle(s *TableStyle) *Table  { t.style = s; return t }
func (t *Table) SetHeaderTitle(s string) *Table { t.headerTitle = s; return t }
func (t *Table) SetFooterTitle(s string) *Table { t.footerTitle = s; return t }
func (t *Table) SetHorizontal(h bool) *Table    { t.horizontal = h; return t }
func (t *Table) SetHeaders(headers []any) *Table {
	t.headers = [][]any{headers}
	if len(headers) == 0 {
		t.headers = nil
	}
	return t
}

// SetRows sets the rows; each row is a []any or a TableSeparator.
func (t *Table) SetRows(rows []any) *Table { t.rows = rows; return t }

func isSeparator(r any) bool { _, ok := r.(TableSeparator); return ok }

var tableDivider = &struct{ divider bool }{true}

// Render ports Table::render().
func (t *Table) Render() {
	var rows []any
	if t.horizontal {
		if len(t.headers) > 0 {
			for i, header := range t.headers[0] {
				r := []any{header}
				for _, row := range t.rows {
					if isSeparator(row) {
						continue
					}
					cells := row.([]any)
					if i < len(cells) {
						r = append(r, cells[i])
					} else if colspan(header) >= 2 {
						// colspan headers take the whole row
					} else {
						r = append(r, nil)
					}
				}
				rows = append(rows, r)
			}
		}
	} else {
		for _, h := range t.headers {
			rows = append(rows, h)
		}
		rows = append(rows, tableDivider)
		rows = append(rows, t.rows...)
	}
	t.calculateNumberOfColumns(rows)
	groups := t.buildTableRows(rows)
	t.calculateColumnsWidth(groups)

	isHeader := !t.horizontal
	isFirstRow := t.horizontal
	hasTitle := t.headerTitle != ""
	var previousRow []any
	pendingSeparators := 0
	for _, group := range groups {
		headerSeparatorRendered := false
		for _, row := range group {
			if row == tableDivider {
				isHeader = false
				isFirstRow = true
				continue
			}
			if isSeparator(row) {
				pendingSeparators++
				continue
			}
			cells := row.([]any)
			if len(cells) == 0 {
				continue
			}
			for ; pendingSeparators > 0; pendingSeparators-- {
				t.renderRowSeparator(sepMid, "", "", previousRow, cells)
			}
			if isHeader && !headerSeparatorRendered && t.style.DisplayOutsideBorder {
				title, format := "", ""
				if hasTitle {
					title, format = t.headerTitle, t.style.HeaderTitleFormat
				}
				t.renderRowSeparator(sepTop, title, format, nil, cells)
				hasTitle = false
				headerSeparatorRendered = true
			}
			if isFirstRow {
				typ := sepTopBottom
				if t.horizontal || !t.hasHeaders() {
					typ = sepTop
				}
				title, format := "", ""
				if hasTitle {
					title, format = t.headerTitle, t.style.HeaderTitleFormat
				}
				t.renderRowSeparator(typ, title, format, previousRow, cells)
				isFirstRow = false
				hasTitle = false
			}
			if t.horizontal {
				t.renderRow(cells, t.style.CellRowFormat, t.style.CellHeaderFormat)
			} else if isHeader {
				t.renderRow(cells, t.style.CellHeaderFormat, "")
			} else {
				t.renderRow(cells, t.style.CellRowFormat, "")
			}
			previousRow = cells
		}
	}
	for ; pendingSeparators > 0; pendingSeparators-- {
		t.renderRowSeparator(sepMid, "", "", previousRow, nil)
	}
	if t.style.DisplayOutsideBorder {
		t.renderRowSeparator(sepBottom, t.footerTitle, t.style.FooterTitleFormat, previousRow, nil)
	}
	t.effectiveWidths = nil
	t.numberOfColumns = 0
}

func (t *Table) hasHeaders() bool {
	for _, h := range t.headers {
		if len(h) > 0 {
			return true
		}
	}
	return false
}

const (
	sepTop = iota
	sepTopBottom
	sepMid
	sepBottom
)

func (t *Table) renderRowSeparator(typ int, title, titleFormat string, rowAbove, rowBelow []any) {
	count := t.numberOfColumns
	if count == 0 {
		return
	}
	s := t.style
	if s.HorizontalOutside == "" && s.HorizontalInside == "" && s.Crossings[0] == "" {
		return
	}
	c := s.Crossings
	var horizontal, left, mid, right string
	switch typ {
	case sepMid:
		horizontal, left, mid, right = s.HorizontalInside, c[8], c[0], c[4]
	case sepTop:
		horizontal, left, mid, right = s.HorizontalOutside, c[1], c[2], c[3]
	case sepTopBottom:
		horizontal, left, mid, right = s.HorizontalOutside, c[9], c[10], c[11]
	default:
		horizontal, left, mid, right = s.HorizontalOutside, c[7], c[6], c[5]
	}
	hasLineAbove := typ != sepTop
	hasLineBelow := typ != sepBottom
	var above, below map[int]bool
	if rowAbove != nil {
		above = t.rowColumns(rowAbove)
	}
	if rowBelow != nil {
		below = t.rowColumns(rowBelow)
	}
	markup := left
	for col := 0; col < count; col++ {
		markup += strings.Repeat(horizontal, t.effectiveWidths[col])
		if col == count-1 {
			markup += right
			continue
		}
		borderAbove := hasLineAbove && (above == nil || above[col+1])
		borderBelow := hasLineBelow && (below == nil || below[col+1])
		switch {
		case borderAbove && borderBelow:
			markup += mid
		case borderBelow:
			markup += c[2]
		case borderAbove:
			markup += c[6]
		default:
			markup += horizontal
		}
	}
	if title != "" {
		f := t.output.Formatter()
		formattedTitle := strings.Replace(titleFormat, "%s", title, 1)
		titleLength := Width(RemoveDecoration(f, formattedTitle))
		markupLength := Width(markup)
		if limit := markupLength - 4; titleLength > limit {
			titleLength = limit
			formatLength := Width(RemoveDecoration(f, strings.Replace(titleFormat, "%s", "", 1)))
			formattedTitle = strings.Replace(titleFormat, "%s", Substr(title, 0, limit-formatLength-3)+"...", 1)
		}
		titleStart := (markupLength - titleLength) / 2
		runes := []rune(markup)
		markup = string(runes[:titleStart]) + formattedTitle + string(runes[min(titleStart+titleLength, len(runes)):])
	}
	t.output.Writeln(strings.Replace(s.BorderFormat, "%s", markup, 1), OutputNormal)
}

func (t *Table) renderColumnSeparator(outside bool) string {
	ch := t.style.VerticalInside
	if outside {
		ch = t.style.VerticalOutside
	}
	return strings.Replace(t.style.BorderFormat, "%s", ch, 1)
}

func (t *Table) rowColumns(row []any) map[int]bool {
	cols := map[int]bool{}
	for i := 0; i < t.numberOfColumns; i++ {
		cols[i] = true
	}
	for key, cell := range row {
		if span := colspan(cell); span > 1 {
			for i := key + 1; i <= key+span-1; i++ {
				delete(cols, i)
			}
		}
	}
	return cols
}

func (t *Table) renderRow(row []any, cellFormat, firstCellFormat string) {
	content := t.renderColumnSeparator(true)
	cols := t.rowColumns(row)
	var ordered []int
	for i := 0; i < t.numberOfColumns; i++ {
		if cols[i] {
			ordered = append(ordered, i)
		}
	}
	for idx, col := range ordered {
		if firstCellFormat != "" && col == 0 {
			content += t.renderCell(row, col, firstCellFormat)
		} else {
			content += t.renderCell(row, col, cellFormat)
		}
		content += t.renderColumnSeparator(idx == len(ordered)-1)
	}
	t.output.Writeln(content, OutputNormal)
}

func (t *Table) columnSeparatorWidth() int {
	return Width(strings.Replace(t.style.BorderFormat, "%s", t.style.VerticalInside, 1))
}

func (t *Table) renderCell(row []any, column int, cellFormat string) string {
	var raw any
	if column < len(row) {
		raw = row[column]
	}
	cell := cellString(raw)
	width := t.effectiveWidths[column]
	if span := colspan(raw); span > 1 {
		for next := column + 1; next <= column+span-1; next++ {
			width += t.columnSeparatorWidth() + t.effectiveWidths[next]
		}
	}
	width += len(cell) - Width(cell) - strings.Count(cell, "\x00")
	width += Length(cell) - Length(RemoveDecoration(t.output.Formatter(), cell))
	content := strings.Replace(t.style.CellRowContentFormat, "%s", cell, 1)
	padded := strPad(content, width, t.style.PaddingChar, t.style.PadType)
	return strings.Replace(cellFormat, "%s", padded, 1)
}

func numberOfColumns(row []any) int {
	n := len(row)
	for _, c := range row {
		n += colspan(c) - 1
	}
	return n
}

func (t *Table) calculateNumberOfColumns(rows []any) {
	n := 0
	for _, r := range rows {
		cells, ok := r.([]any)
		if !ok {
			continue
		}
		if c := numberOfColumns(cells); c > n {
			n = c
		}
	}
	t.numberOfColumns = n
}

// buildTableRows ports Table::buildTableRows(): multi-line cells are split into extra rows in their row group.
func (t *Table) buildTableRows(rows []any) [][]any {
	unmerged := map[int]map[int][]any{}
	for rowKey, r := range rows {
		cells, ok := r.([]any)
		if !ok {
			continue
		}
		for column, raw := range cells {
			cell := cellString(raw)
			if !strings.Contains(cell, "\n") {
				continue
			}
			span := colspan(raw)
			eol := "\n"
			if strings.Contains(cell, "\r\n") {
				eol = "\r\n"
			}
			parts := strings.Split(cell, eol)
			for i, p := range parts {
				parts[i] = EscapeTrailingBackslash(p)
			}
			escaped := strings.Join(parts, eol)
			lines := strings.Split(strings.ReplaceAll(escaped, eol, "<fg=default;bg=default></>"+eol), eol)
			for lineKey, line := range lines {
				var value any = line
				if span > 1 {
					value = &TableCell{Value: line, Colspan: span}
				}
				if lineKey == 0 {
					cells[column] = value
					continue
				}
				if unmerged[rowKey] == nil {
					unmerged[rowKey] = map[int][]any{}
				}
				if _, ok := unmerged[rowKey][lineKey]; !ok {
					unmerged[rowKey][lineKey] = copyRow(cells)
				}
				unmerged[rowKey][lineKey][column] = value
			}
		}
	}
	var groups [][]any
	for rowKey, r := range rows {
		group := []any{t.fillCellsOrSeparator(r)}
		if extra, ok := unmerged[rowKey]; ok {
			keys := make([]int, 0, len(extra))
			for k := range extra {
				keys = append(keys, k)
			}
			sort.Ints(keys)
			for _, k := range keys {
				group = append(group, t.fillCellsOrSeparator(extra[k]))
			}
		}
		groups = append(groups, group)
	}
	return groups
}

func copyRow(cells []any) []any {
	row := make([]any, len(cells))
	for i, c := range cells {
		row[i] = ""
		if tc, ok := c.(*TableCell); ok {
			row[i] = &TableCell{Colspan: tc.Colspan}
		}
	}
	return row
}

func (t *Table) fillCellsOrSeparator(r any) any {
	cells, ok := r.([]any)
	if !ok {
		return r
	}
	var out []any
	for _, c := range cells {
		out = append(out, c)
		for i := 1; i < colspan(c); i++ {
			out = append(out, "")
		}
	}
	if out == nil {
		return cells
	}
	return out
}

func (t *Table) calculateColumnsWidth(groups [][]any) {
	f := t.output.Formatter()
	maxWidths := make([]int, t.numberOfColumns)
	for _, group := range groups {
		for _, r := range group {
			cells, ok := r.([]any)
			if !ok {
				continue
			}
			row := append([]any(nil), cells...)
			for i, c := range cells {
				if span := colspan(c); span > 1 {
					text := RemoveDecoration(f, cellString(c))
					if l := Width(text); l > 0 {
						size := (l + span - 1) / span
						runes := []rune(text)
						for pos := 0; pos*size < len(runes); pos++ {
							end := min((pos+1)*size, len(runes))
							for len(row) <= i+pos {
								row = append(row, nil)
							}
							row[i+pos] = string(runes[pos*size : end])
						}
					}
				}
			}
			for col := 0; col < t.numberOfColumns; col++ {
				w := 0
				if col < len(row) && row[col] != nil {
					w = Width(RemoveDecoration(f, cellString(row[col])))
				}
				if cw := t.columnWidths[col]; cw > w {
					w = cw
				}
				if w > maxWidths[col] {
					maxWidths[col] = w
				}
			}
		}
	}
	contentFormatWidth := Width(t.style.CellRowContentFormat) - 2
	t.effectiveWidths = make([]int, t.numberOfColumns)
	for col := range maxWidths {
		t.effectiveWidths[col] = maxWidths[col] + contentFormatWidth
	}
}
