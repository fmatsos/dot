package settings

import (
	"fmt"
	"strings"
)

// Diff renders a unified diff without context lines (diff -U0) between two texts,
// labelled like `diff --label`. It returns "" when the texts are equal.
// ponytail: quadratic LCS on the lines left after trimming the common ends; settings files are small.
func Diff(labelA, labelB, a, b string) string {
	if a == b {
		return ""
	}
	x, y := splitLines(a), splitLines(b)
	pre := 0
	for pre < len(x) && pre < len(y) && x[pre] == y[pre] {
		pre++
	}
	suf := 0
	for suf < len(x)-pre && suf < len(y)-pre && x[len(x)-1-suf] == y[len(y)-1-suf] {
		suf++
	}
	mx, my := x[pre:len(x)-suf], y[pre:len(y)-suf]
	// lcs[i][j]: length of the LCS of mx[i:] and my[j:].
	lcs := make([][]int, len(mx)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(my)+1)
	}
	for i := len(mx) - 1; i >= 0; i-- {
		for j := len(my) - 1; j >= 0; j-- {
			if mx[i] == my[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	same := func(i, j int) bool { return i < len(mx) && j < len(my) && mx[i] == my[j] }
	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n+++ %s\n", labelA, labelB)
	i, j := 0, 0
	for i < len(mx) || j < len(my) {
		if same(i, j) {
			i++
			j++
			continue
		}
		// One hunk: deletions and insertions up to the next common line.
		si, sj := i, j
		for (i < len(mx) || j < len(my)) && !same(i, j) {
			if j >= len(my) || (i < len(mx) && lcs[i+1][j] >= lcs[i][j+1]) {
				i++
			} else {
				j++
			}
		}
		fmt.Fprintf(&sb, "@@ -%s +%s @@\n", hunkRange(pre+si, i-si), hunkRange(pre+sj, j-sj))
		for _, l := range mx[si:i] {
			sb.WriteString("-" + l + "\n")
		}
		for _, l := range my[sj:j] {
			sb.WriteString("+" + l + "\n")
		}
	}
	return sb.String()
}

// hunkRange formats a hunk range from a 0-based offset and a line count, the way diff prints it.
func hunkRange(off, n int) string {
	switch n {
	case 0:
		return fmt.Sprintf("%d,0", off)
	case 1:
		return fmt.Sprintf("%d", off+1)
	}
	return fmt.Sprintf("%d,%d", off+1, n)
}

func splitLines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
