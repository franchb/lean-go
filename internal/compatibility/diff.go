package compatibility

import (
	"fmt"
	"strings"
)

// splitLines splits text into lines that keep their "\n".
func splitLines(text string) []string {
	var out []string
	for text != "" {
		i := strings.IndexByte(text, '\n')
		if i < 0 {
			out = append(out, text)
			break
		}
		out = append(out, text[:i+1])
		text = text[i+1:]
	}
	return out
}

type edit struct {
	kind byte // ' ', '-', '+'
	line string
}

// myers returns a shortest edit script from a to b (Myers' O(ND) algorithm).
func myers(a, b []string) []edit {
	n, m := len(a), len(b)
	max := n + m
	offset := max + 1
	v := make([]int, 2*max+3)
	var trace [][]int
	for d := 0; d <= max; d++ {
		snapshot := make([]int, len(v))
		copy(snapshot, v)
		trace = append(trace, snapshot)
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[offset+k-1] < v[offset+k+1]) {
				x = v[offset+k+1]
			} else {
				x = v[offset+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[offset+k] = x
			if x >= n && y >= m {
				return backtrack(a, b, trace, offset, d)
			}
		}
	}
	return nil
}

func backtrack(a, b []string, trace [][]int, offset, d int) []edit {
	x, y := len(a), len(b)
	var out []edit
	for ; d > 0; d-- {
		v := trace[d]
		k := x - y
		var prevK int
		if k == -d || (k != d && v[offset+k-1] < v[offset+k+1]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := v[offset+prevK]
		prevY := prevX - prevK
		for x > prevX && y > prevY {
			x--
			y--
			out = append(out, edit{' ', a[x]})
		}
		if x == prevX {
			y--
			out = append(out, edit{'+', b[y]})
		} else {
			x--
			out = append(out, edit{'-', a[x]})
		}
	}
	for x > 0 && y > 0 {
		x--
		y--
		out = append(out, edit{' ', a[x]})
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// unifiedRange formats a hunk range as difflib does.
func unifiedRange(start, length int) string {
	beginning := start + 1
	if length == 1 {
		return fmt.Sprint(beginning)
	}
	if length == 0 {
		beginning--
	}
	return fmt.Sprintf("%d,%d", beginning, length)
}

// UnifiedDiff returns a unified diff with three lines of context; it is empty
// for equal texts.
func UnifiedDiff(before, after, fromFile, toFile string) string {
	edits := myers(splitLines(before), splitLines(after))
	changed := false
	for _, e := range edits {
		if e.kind != ' ' {
			changed = true
			break
		}
	}
	if !changed {
		return ""
	}
	const context = 3
	var out strings.Builder
	fmt.Fprintf(&out, "--- %s\n+++ %s\n", fromFile, toFile)
	// Positions of each edit in a and b.
	aPos, bPos := make([]int, len(edits)+1), make([]int, len(edits)+1)
	for i, e := range edits {
		aPos[i+1], bPos[i+1] = aPos[i], bPos[i]
		if e.kind != '+' {
			aPos[i+1]++
		}
		if e.kind != '-' {
			bPos[i+1]++
		}
	}
	for i := 0; i < len(edits); {
		if edits[i].kind == ' ' {
			i++
			continue
		}
		start := max(0, i-context)
		end := i
		// Extend the hunk while changes are within 2*context lines.
		for j := i; j < len(edits); j++ {
			if edits[j].kind != ' ' {
				end = j + 1
			} else if j-end >= 2*context {
				break
			}
		}
		stop := min(len(edits), end+context)
		fmt.Fprintf(&out, "@@ -%s +%s @@\n", unifiedRange(aPos[start], aPos[stop]-aPos[start]),
			unifiedRange(bPos[start], bPos[stop]-bPos[start]))
		for _, e := range edits[start:stop] {
			line := e.line
			if !strings.HasSuffix(line, "\n") {
				line += "\n"
			}
			out.WriteByte(e.kind)
			out.WriteString(line)
		}
		i = stop
	}
	return out.String()
}
