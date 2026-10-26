package app

import "strings"

// joinFixedColumns stacks title, columns side by side, and status without
// re-measuring cells; ok is false when the columns' line counts disagree.
func joinFixedColumns(title, status string, columns []string) (string, bool) {
	split := make([][]string, len(columns))
	n := 0
	for i, c := range columns {
		split[i] = strings.Split(c, "\n")
		if len(split[i]) != len(split[0]) {
			return "", false
		}
		n += len(c)
	}
	var b strings.Builder
	b.Grow(len(title) + len(status) + n + 2*len(split[0]))
	b.WriteString(title)
	for row := range split[0] {
		b.WriteByte('\n')
		for _, col := range split {
			b.WriteString(col[row])
		}
	}
	b.WriteByte('\n')
	b.WriteString(status)
	return b.String(), true
}
