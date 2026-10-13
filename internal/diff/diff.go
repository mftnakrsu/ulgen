// Package diff produces unified diffs for permission prompts and tool results.
package diff

import (
	"fmt"
	"strings"
)

// context is the number of unchanged lines shown around a change.
const context = 3

// maxCells bounds the dynamic-programming table; above it the changed
// region is shown as one block of removals and additions.
const maxCells = 4_000_000

// Unified renders the difference between two texts in unified format. Line
// endings are compared without a trailing carriage return, so a CRLF file
// diffs like an LF file. An empty result means the texts are equal.
func Unified(oldName, newName, oldText, newText string) string {
	a, b := lines(oldText), lines(newText)
	ops := compute(a, b)
	if !hasChange(ops) {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n+++ %s\n", oldName, newName)
	writeHunks(&sb, a, b, ops)
	return sb.String()
}

// Stat summarises a change as "+added -removed" line counts.
func Stat(oldText, newText string) (added, removed int) {
	for _, op := range compute(lines(oldText), lines(newText)) {
		switch op.kind {
		case opAdd:
			added++
		case opDel:
			removed++
		}
	}
	return added, removed
}

func lines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}

type opKind int

const (
	opEq opKind = iota
	opDel
	opAdd
)

// op is one line of the edit script: an equal, removed or added line,
// with its indexes in a (old) and b (new); -1 where it does not apply.
type op struct {
	kind opKind
	ai   int
	bi   int
}

func hasChange(ops []op) bool {
	for _, o := range ops {
		if o.kind != opEq {
			return true
		}
	}
	return false
}

// compute returns the edit script from a to b. Common prefix and suffix
// lines are matched first; the remaining middle goes through a longest
// common subsequence table when it is small enough.
func compute(a, b []string) []op {
	var ops []op
	start := 0
	for start < len(a) && start < len(b) && a[start] == b[start] {
		ops = append(ops, op{opEq, start, start})
		start++
	}
	endA, endB := len(a), len(b)
	for endA > start && endB > start && a[endA-1] == b[endB-1] {
		endA--
		endB--
	}
	midA, midB := a[start:endA], b[start:endB]
	if len(midA)*len(midB) <= maxCells {
		for _, o := range lcs(midA, midB) {
			if o.ai >= 0 {
				o.ai += start
			}
			if o.bi >= 0 {
				o.bi += start
			}
			ops = append(ops, o)
		}
	} else {
		for i := range midA {
			ops = append(ops, op{opDel, start + i, -1})
		}
		for j := range midB {
			ops = append(ops, op{opAdd, -1, start + j})
		}
	}
	for i := 0; endA+i < len(a); i++ {
		ops = append(ops, op{opEq, endA + i, endB + i})
	}
	return ops
}

// lcs computes an edit script through a longest-common-subsequence table.
func lcs(a, b []string) []op {
	n, m := len(a), len(b)
	table := make([][]int32, n+1)
	for i := range table {
		table[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				table[i][j] = table[i+1][j+1] + 1
			} else if table[i+1][j] >= table[i][j+1] {
				table[i][j] = table[i+1][j]
			} else {
				table[i][j] = table[i][j+1]
			}
		}
	}
	var ops []op
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, op{opEq, i, j})
			i++
			j++
		case table[i+1][j] >= table[i][j+1]:
			ops = append(ops, op{opDel, i, -1})
			i++
		default:
			ops = append(ops, op{opAdd, -1, j})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, op{opDel, i, -1})
	}
	for ; j < m; j++ {
		ops = append(ops, op{opAdd, -1, j})
	}
	return ops
}

// writeHunks groups the edit script into hunks separated by more than
// 2*context unchanged lines and renders them.
func writeHunks(sb *strings.Builder, a, b []string, ops []op) {
	for i := 0; i < len(ops); {
		// Find the next change.
		for i < len(ops) && ops[i].kind == opEq {
			i++
		}
		if i >= len(ops) {
			return
		}
		hunkStart := max(i-context, 0)
		// Extend the hunk while changes are close together.
		end := i
		for end < len(ops) {
			if ops[end].kind != opEq {
				end++
				continue
			}
			// Count the run of equal lines.
			run := end
			for run < len(ops) && ops[run].kind == opEq {
				run++
			}
			if run >= len(ops) || run-end > 2*context {
				break
			}
			end = run
		}
		hunkEnd := min(end+context, len(ops))
		hunk := ops[hunkStart:hunkEnd]
		oldStart, oldCount, newStart, newCount := ranges(hunk)
		fmt.Fprintf(sb, "@@ -%s +%s @@\n", rangeText(oldStart, oldCount), rangeText(newStart, newCount))
		for _, o := range hunk {
			switch o.kind {
			case opEq:
				sb.WriteString(" " + a[o.ai] + "\n")
			case opDel:
				sb.WriteString("-" + a[o.ai] + "\n")
			case opAdd:
				sb.WriteString("+" + b[o.bi] + "\n")
			}
		}
		i = hunkEnd
	}
}

func ranges(hunk []op) (oldStart, oldCount, newStart, newCount int) {
	oldStart, newStart = -1, -1
	for _, o := range hunk {
		if o.ai >= 0 {
			if oldStart < 0 {
				oldStart = o.ai
			}
			oldCount++
		}
		if o.bi >= 0 {
			if newStart < 0 {
				newStart = o.bi
			}
			newCount++
		}
	}
	// Unified diffs number lines from 1; an empty side reports the line before it.
	if oldStart < 0 {
		oldStart = firstIndex(hunk, true)
	}
	if newStart < 0 {
		newStart = firstIndex(hunk, false)
	}
	return oldStart + 1, oldCount, newStart + 1, newCount
}

// firstIndex estimates where an empty side sits: at the position of the
// other side's first line, so the hunk header stays plausible.
func firstIndex(hunk []op, old bool) int {
	for _, o := range hunk {
		if old && o.bi >= 0 {
			return o.bi - 1
		}
		if !old && o.ai >= 0 {
			return o.ai - 1
		}
	}
	return -1
}

func rangeText(start, count int) string {
	if count == 1 {
		return fmt.Sprintf("%d", start)
	}
	if count == 0 {
		return fmt.Sprintf("%d,0", start)
	}
	return fmt.Sprintf("%d,%d", start, count)
}
