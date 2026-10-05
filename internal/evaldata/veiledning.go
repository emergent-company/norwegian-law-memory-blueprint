package evaldata

import (
	"strings"
	"unicode"
)

// SplitPoints splits sensorveiledning text into natural paragraphs / numbered
// bullets. Each returned element is trimmed. Header/footer noise and empty
// lines are dropped.
//
// Rules (deterministic, conservative):
//   - split on blank lines first;
//   - within a blank-line-separated block, split again on numbered bullet
//     markers ("1.", "1)", "a)", "1.1", "§ 1", etc.) and on hard line breaks
//     when the block is long, so each gold point stays self-contained.
func SplitPoints(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	blocks := strings.Split(text, "\n\n")
	var out []string
	for _, block := range blocks {
		for _, part := range splitBlock(block) {
			s := strings.TrimSpace(part)
			if s == "" || isNoiseLine(s) {
				continue
			}
			out = append(out, s)
		}
	}
	return out
}

// splitBlock further subdivides a single block into bullet items.
func splitBlock(block string) []string {
	lines := strings.Split(block, "\n")
	var parts []string
	var buf []string
	flush := func() {
		if len(buf) > 0 {
			parts = append(parts, strings.TrimSpace(strings.Join(buf, " ")))
			buf = buf[:0]
		}
	}
	for _, line := range lines {
		if isBulletLine(line) && len(buf) > 0 {
			flush()
		}
		buf = append(buf, line)
	}
	flush()
	if len(parts) == 0 {
		return []string{block}
	}
	return parts
}

// isBulletLine reports whether a line starts a new numbered/lettered bullet.
func isBulletLine(line string) bool {
	t := strings.TrimSpace(line)
	if t == "" {
		return false
	}
	rs := []rune(t)
	// Lettered bullet: "a)" or "a." followed by space/end.
	if len(rs) >= 2 && isLowerLetter(rs[0]) && (rs[1] == ')' || rs[1] == '.') {
		if len(rs) == 2 || rs[2] == ' ' || rs[2] == '\t' {
			return true
		}
	}
	// Numbered bullet: "1." / "1)" / "1.1" / "§ 1" / "§ 1.2".
	i := 0
	if rs[0] == '§' {
		i = 1
		for i < len(rs) && (rs[i] == ' ' || rs[i] == '\t') {
			i++
		}
	}
	if i < len(rs) && unicode.IsDigit(rs[i]) {
		j := i
		for j < len(rs) && (unicode.IsDigit(rs[j]) || rs[j] == '.') {
			j++
		}
		if j < len(rs) && (rs[j] == ')' || rs[j] == '.' || rs[j] == ' ' || rs[j] == '\t') {
			return true
		}
	}
	return false
}

func isLowerLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || r == 'æ' || r == 'ø' || r == 'å'
}

// isNoiseLine reports whether a trimmed line is likely header/footer noise.
func isNoiseLine(s string) bool {
	low := strings.ToLower(s)
	if strings.Contains(low, "sensorveiledning") && len(s) < 60 {
		return true
	}
	if strings.Contains(low, "side ") && strings.Contains(low, " av ") {
		return true
	}
	if s == "1" || s == "2" || s == "3" { // stray page numbers
		return true
	}
	return false
}
