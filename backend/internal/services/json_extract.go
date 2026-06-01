package services

import "strings"

func extractJSON(text string) string {
	start := strings.Index(text, "{")
	if start == -1 {
		return text
	}
	depth := 0
	for i := start; i < len(text); i++ {
		switch text[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[start : i+1]
			}
		}
	}
	return text[start:]
}
