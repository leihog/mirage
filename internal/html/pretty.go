package html

import (
	"html/template"
	"strings"
)

func PrettyHTMLSource(source string) template.HTML {
	if strings.TrimSpace(source) == "" {
		return template.HTML(`<span class="html-muted">(no HTML body)</span>`)
	}
	return template.HTML(colorHTMLSource(prettyHTMLSource(source)))
}

func prettyHTMLSource(source string) string {
	source = strings.TrimSpace(source)
	var out strings.Builder
	indent := 0

	writeLine := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if out.Len() > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(strings.Repeat("  ", max(indent, 0)))
		out.WriteString(value)
	}

	for i := 0; i < len(source); {
		if source[i] != '<' {
			next := strings.IndexByte(source[i:], '<')
			if next == -1 {
				next = len(source) - i
			}
			text := strings.Join(strings.Fields(source[i:i+next]), " ")
			if text != "" {
				writeLine(text)
			}
			i += next
			continue
		}

		end := htmlTagEnd(source, i)
		if end == -1 {
			writeLine(source[i:])
			break
		}

		tag := strings.TrimSpace(source[i : end+1])
		if htmlClosingTag(tag) {
			indent--
		}
		writeLine(tag)
		if htmlOpeningTag(tag) && !htmlVoidTag(tag) {
			indent++
		}
		i = end + 1
	}

	return out.String()
}

func htmlTagEnd(source string, start int) int {
	var quote byte
	for i := start + 1; i < len(source); i++ {
		switch source[i] {
		case '\'', '"':
			if quote == 0 {
				quote = source[i]
			} else if quote == source[i] {
				quote = 0
			}
		case '>':
			if quote == 0 {
				return i
			}
		}
	}
	return -1
}

func htmlClosingTag(tag string) bool {
	return strings.HasPrefix(tag, "</")
}

func htmlOpeningTag(tag string) bool {
	if !strings.HasPrefix(tag, "<") || htmlClosingTag(tag) || strings.HasPrefix(tag, "<!") || strings.HasPrefix(tag, "<?") {
		return false
	}
	return !strings.HasSuffix(strings.TrimSpace(tag), "/>")
}

func htmlVoidTag(tag string) bool {
	switch htmlTagName(tag) {
	case "area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "param", "source", "track", "wbr":
		return true
	default:
		return false
	}
}

func htmlTagName(tag string) string {
	tag = strings.TrimSpace(tag)
	tag = strings.TrimPrefix(tag, "</")
	tag = strings.TrimPrefix(tag, "<")
	tag = strings.TrimLeft(tag, "!?")
	end := strings.IndexFunc(tag, func(r rune) bool {
		return r == '>' || r == '/' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	if end == -1 {
		end = len(tag)
	}
	return strings.ToLower(tag[:end])
}

func colorHTMLSource(source string) string {
	var out strings.Builder
	for i := 0; i < len(source); {
		if source[i] != '<' {
			next := strings.IndexByte(source[i:], '<')
			if next == -1 {
				next = len(source) - i
			}
			out.WriteString(`<span class="html-text">`)
			out.WriteString(template.HTMLEscapeString(source[i : i+next]))
			out.WriteString(`</span>`)
			i += next
			continue
		}

		end := htmlTagEnd(source, i)
		if end == -1 {
			out.WriteString(template.HTMLEscapeString(source[i:]))
			break
		}
		out.WriteString(colorHTMLTag(source[i : end+1]))
		i = end + 1
	}
	return out.String()
}

func colorHTMLTag(tag string) string {
	if strings.HasPrefix(tag, "<!--") {
		return `<span class="html-comment">` + template.HTMLEscapeString(tag) + `</span>`
	}
	if strings.HasPrefix(tag, "<!") || strings.HasPrefix(tag, "<?") {
		return `<span class="html-punctuation">` + template.HTMLEscapeString(tag) + `</span>`
	}

	var out strings.Builder
	i := 0
	for i < len(tag) {
		if tag[i] == '<' || tag[i] == '>' || tag[i] == '/' || tag[i] == '=' {
			out.WriteString(`<span class="html-punctuation">`)
			out.WriteString(template.HTMLEscapeString(tag[i : i+1]))
			out.WriteString(`</span>`)
			i++
			continue
		}
		if tag[i] == '"' || tag[i] == '\'' {
			end := quotedHTMLValueEnd(tag, i)
			out.WriteString(`<span class="html-attr-value">`)
			out.WriteString(template.HTMLEscapeString(tag[i:end]))
			out.WriteString(`</span>`)
			i = end
			continue
		}
		if isHTMLSpace(tag[i]) {
			out.WriteByte(tag[i])
			i++
			continue
		}

		start := i
		for i < len(tag) && !isHTMLSpace(tag[i]) && !strings.ContainsRune(`<>/="'`, rune(tag[i])) {
			i++
		}
		class := "html-attr-name"
		if previousNonSpace(tag, start) == '<' || previousNonSpace(tag, start) == '/' && start > 1 && tag[start-2] == '<' {
			class = "html-tag-name"
		}
		out.WriteString(`<span class="` + class + `">`)
		out.WriteString(template.HTMLEscapeString(tag[start:i]))
		out.WriteString(`</span>`)
	}
	return out.String()
}

func quotedHTMLValueEnd(tag string, start int) int {
	quote := tag[start]
	for i := start + 1; i < len(tag); i++ {
		if tag[i] == quote {
			return i + 1
		}
	}
	return len(tag)
}

func previousNonSpace(value string, before int) byte {
	for i := before - 1; i >= 0; i-- {
		if !isHTMLSpace(value[i]) {
			return value[i]
		}
	}
	return 0
}

func isHTMLSpace(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r'
}
