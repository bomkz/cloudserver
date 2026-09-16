package jsonstore

import (
	"fmt"
	"strconv"
	"strings"
)

// This file implements a small, dependency-free text diff engine:
// Myers' shortest-edit-script algorithm to find the minimal set of line
// changes, then rendering/parsing that as unified-diff hunks (the same
// "@@ -a,b +c,d @@" format `diff -u` and `git diff` use). Only lines
// actually touched, plus a little surrounding context, are ever written to
// disk — long unchanged stretches are represented purely by the hunk
// header's line numbers, which is what keeps saved diffs small.

type opType int

const (
	opEqual opType = iota
	opDelete
	opInsert
)

type op struct {
	kind opType
	text string
}

// splitLines splits canonical JSON text (always "\n"-terminated, or empty)
// into lines without a trailing empty element.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}

// joinLines is the inverse of splitLines.
func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// myersDiff returns the shortest edit script turning a into b, using
// Myers' O((N+M)D) algorithm (Myers, "An O(ND) Difference Algorithm and
// Its Variations", 1986).
func myersDiff(a, b []string) []op {
	n, m := len(a), len(b)
	if n == 0 && m == 0 {
		return nil
	}
	max := n + m
	v := map[int]int{1: 0}
	trace := make([]map[int]int, 0, max+1)

	for d := 0; d <= max; d++ {
		snapshot := make(map[int]int, len(v))
		for k, val := range v {
			snapshot[k] = val
		}
		trace = append(trace, snapshot)

		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[k-1] < v[k+1]) {
				x = v[k+1]
			} else {
				x = v[k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[k] = x
			if x >= n && y >= m {
				return backtrack(a, b, trace, d)
			}
		}
	}
	return nil // unreachable: the loop above always finds the endpoint by d==max
}

// backtrack walks the recorded V-arrays from myersDiff backwards to
// reconstruct the actual sequence of equal/insert/delete operations.
func backtrack(a, b []string, trace []map[int]int, d int) []op {
	x, y := len(a), len(b)
	var ops []op

	for D := d; D > 0; D-- {
		v := trace[D]
		k := x - y
		var prevK int
		if k == -D || (k != D && v[k-1] < v[k+1]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := v[prevK]
		prevY := prevX - prevK

		for x > prevX && y > prevY {
			ops = append(ops, op{opEqual, a[x-1]})
			x--
			y--
		}
		if x == prevX {
			ops = append(ops, op{opInsert, b[y-1]})
			y--
		} else {
			ops = append(ops, op{opDelete, a[x-1]})
			x--
		}
	}
	for x > 0 && y > 0 {
		ops = append(ops, op{opEqual, a[x-1]})
		x--
		y--
	}
	for i, j := 0, len(ops)-1; i < j; i, j = i+1, j-1 {
		ops[i], ops[j] = ops[j], ops[i]
	}
	return ops
}

type hunk struct {
	aStart, aLen int
	bStart, bLen int
	ops          []op
}

// buildHunks groups an edit script into hunks, merging nearby changes and
// keeping only `context` unchanged lines around each one — everything
// further away is simply skipped, its position implied by the hunk header.
func buildHunks(ops []op, context int) []hunk {
	var changeIdx []int
	for i, o := range ops {
		if o.kind != opEqual {
			changeIdx = append(changeIdx, i)
		}
	}
	if len(changeIdx) == 0 {
		return nil
	}

	type span struct{ lo, hi int }
	clampHi := len(ops) - 1
	lo := clamp0(changeIdx[0] - context)
	hi := clampMax(changeIdx[0]+context, clampHi)
	var spans []span
	for _, ci := range changeIdx[1:] {
		newLo := clamp0(ci - context)
		newHi := clampMax(ci+context, clampHi)
		if newLo <= hi+1 {
			if newHi > hi {
				hi = newHi
			}
			continue
		}
		spans = append(spans, span{lo, hi})
		lo, hi = newLo, newHi
	}
	spans = append(spans, span{lo, hi})

	// aPos[i]/bPos[i] = number of a/b lines consumed by ops[:i].
	aPos := make([]int, len(ops)+1)
	bPos := make([]int, len(ops)+1)
	for i, o := range ops {
		aPos[i+1], bPos[i+1] = aPos[i], bPos[i]
		switch o.kind {
		case opEqual:
			aPos[i+1]++
			bPos[i+1]++
		case opDelete:
			aPos[i+1]++
		case opInsert:
			bPos[i+1]++
		}
	}

	hunks := make([]hunk, 0, len(spans))
	for _, sp := range spans {
		sub := ops[sp.lo : sp.hi+1]
		hunks = append(hunks, hunk{
			aStart: aPos[sp.lo] + 1,
			aLen:   aPos[sp.hi+1] - aPos[sp.lo],
			bStart: bPos[sp.lo] + 1,
			bLen:   bPos[sp.hi+1] - bPos[sp.lo],
			ops:    sub,
		})
	}
	return hunks
}

func clamp0(x int) int {
	if x < 0 {
		return 0
	}
	return x
}

func clampMax(x, max int) int {
	if x > max {
		return max
	}
	return x
}

// unifiedDiff renders the changes needed to turn a into b as unified-diff
// text. Returns "" if a and b are identical.
func unifiedDiff(a, b string, context int) string {
	ops := myersDiff(splitLines(a), splitLines(b))
	hunks := buildHunks(ops, context)
	if len(hunks) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, h := range hunks {
		fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n", h.aStart, h.aLen, h.bStart, h.bLen)
		for _, o := range h.ops {
			switch o.kind {
			case opEqual:
				sb.WriteByte(' ')
			case opDelete:
				sb.WriteByte('-')
			case opInsert:
				sb.WriteByte('+')
			}
			sb.WriteString(o.text)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// applyUnifiedDiff applies patch text (as produced by unifiedDiff) to base,
// returning the resulting text.
func applyUnifiedDiff(base, patch string) (string, error) {
	if strings.TrimSpace(patch) == "" {
		return base, nil
	}
	srcLines := splitLines(base)
	patchLines := strings.Split(patch, "\n")
	if n := len(patchLines); n > 0 && patchLines[n-1] == "" {
		patchLines = patchLines[:n-1]
	}

	var out []string
	srcPos := 0
	i := 0
	for i < len(patchLines) {
		header := patchLines[i]
		if !strings.HasPrefix(header, "@@") {
			return "", fmt.Errorf("jsonstore: expected hunk header, got %q", header)
		}
		aStart, _, _, _, err := parseHunkHeader(header)
		if err != nil {
			return "", err
		}
		target := aStart - 1
		if target < srcPos || target > len(srcLines) {
			return "", fmt.Errorf("jsonstore: hunk header out of range: %q", header)
		}
		out = append(out, srcLines[srcPos:target]...)
		srcPos = target
		i++

		for i < len(patchLines) && !strings.HasPrefix(patchLines[i], "@@") {
			l := patchLines[i]
			if l == "" {
				return "", fmt.Errorf("jsonstore: unexpected blank line in patch")
			}
			switch l[0] {
			case ' ':
				if srcPos >= len(srcLines) || srcLines[srcPos] != l[1:] {
					return "", fmt.Errorf("jsonstore: context mismatch at source line %d", srcPos+1)
				}
				out = append(out, srcLines[srcPos])
				srcPos++
			case '-':
				if srcPos >= len(srcLines) || srcLines[srcPos] != l[1:] {
					return "", fmt.Errorf("jsonstore: delete mismatch at source line %d", srcPos+1)
				}
				srcPos++
			case '+':
				out = append(out, l[1:])
			default:
				return "", fmt.Errorf("jsonstore: invalid patch line: %q", l)
			}
			i++
		}
	}
	out = append(out, srcLines[srcPos:]...)
	return joinLines(out), nil
}

func parseHunkHeader(line string) (aStart, aLen, bStart, bLen int, err error) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return 0, 0, 0, 0, fmt.Errorf("jsonstore: malformed hunk header: %q", line)
	}
	aPart := strings.TrimPrefix(fields[1], "-")
	bPart := strings.TrimPrefix(fields[2], "+")
	aStart, aLen, err = parseRange(aPart)
	if err != nil {
		return
	}
	bStart, bLen, err = parseRange(bPart)
	return
}

func parseRange(s string) (start, length int, err error) {
	parts := strings.SplitN(s, ",", 2)
	start, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("jsonstore: bad range %q: %w", s, err)
	}
	if len(parts) == 2 {
		length, err = strconv.Atoi(parts[1])
		if err != nil {
			return 0, 0, fmt.Errorf("jsonstore: bad range %q: %w", s, err)
		}
	} else {
		length = 1
	}
	return start, length, nil
}
