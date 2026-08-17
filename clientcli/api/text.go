package api

import (
	"fmt"
	"html"
	"strings"
	"time"
)

type Textable interface {
	String() string
	ANSI() string
	HTML() string
	Markdown() string
}

type textPart struct {
	value Textable
	text  string
	style string
}

type Text struct {
	Content string
	Style   string
	parts   []textPart
}

func (t Text) Add(value any) Text {
	return t.Append(value)
}

func (t Text) AddText(value string, styles ...string) Text {
	return t.Append(value, styles...)
}

func (t Text) Append(value any, styles ...string) Text {
	style := strings.Join(styles, " ")
	if textable, ok := value.(Textable); ok {
		t.parts = append(t.parts, textPart{value: textable, style: style})
	} else {
		t.parts = append(t.parts, textPart{text: fmt.Sprint(value), style: style})
	}
	return t
}

func (t Text) Appendf(format string, args ...any) Text {
	return t.Append(fmt.Sprintf(format, args...))
}

func (t Text) NewLine() Text {
	return t.Append("\n")
}

func (t Text) Space() Text {
	return t.Append(" ")
}

func (t Text) Wrap(prefix, suffix string) Text {
	return Text{}.Append(prefix).Add(t).Append(suffix)
}

func (t Text) WithStyle(style string) Text {
	t.Style = style
	return t
}

func (t Text) String() string {
	var b strings.Builder
	b.WriteString(t.Content)
	for _, part := range t.parts {
		if part.value != nil {
			b.WriteString(part.value.String())
		} else {
			b.WriteString(part.text)
		}
	}
	return b.String()
}

func (t Text) ANSI() string {
	var b strings.Builder
	b.WriteString(ansiText(t.Content, t.Style))
	for _, part := range t.parts {
		if part.value != nil {
			value := part.value.ANSI()
			if part.style != "" {
				value = ansiText(part.value.String(), part.style)
			}
			b.WriteString(value)
		} else {
			b.WriteString(ansiText(part.text, part.style))
		}
	}
	return b.String()
}

func (t Text) HTML() string {
	var b strings.Builder
	b.WriteString(htmlText(t.Content, t.Style))
	for _, part := range t.parts {
		if part.value != nil {
			b.WriteString(part.value.HTML())
		} else {
			b.WriteString(htmlText(part.text, part.style))
		}
	}
	return b.String()
}

func (t Text) Markdown() string {
	return t.String()
}

func ansiText(value, style string) string {
	if value == "" || style == "" {
		return value
	}
	codes := make([]string, 0, 2)
	if strings.Contains(style, "font-bold") {
		codes = append(codes, "1")
	}
	switch {
	case strings.Contains(style, "red-"):
		codes = append(codes, "31")
	case strings.Contains(style, "green-"):
		codes = append(codes, "32")
	case strings.Contains(style, "yellow-"), strings.Contains(style, "orange-"):
		codes = append(codes, "33")
	case strings.Contains(style, "blue-"):
		codes = append(codes, "34")
	case strings.Contains(style, "purple-"):
		codes = append(codes, "35")
	case strings.Contains(style, "gray-"), strings.Contains(style, "muted"):
		codes = append(codes, "90")
	}
	if len(codes) == 0 {
		return value
	}
	return "\x1b[" + strings.Join(codes, ";") + "m" + value + "\x1b[0m"
}

func htmlText(value, style string) string {
	value = strings.ReplaceAll(html.EscapeString(value), "\n", "<br>\n")
	if style == "" {
		return value
	}
	return `<span class="` + html.EscapeString(style) + `">` + value + `</span>`
}

func Human(value any, styles ...string) Text {
	if duration, ok := value.(time.Duration); ok {
		return Text{Content: duration.String(), Style: strings.Join(styles, " ")}
	}
	return Text{Content: fmt.Sprint(value), Style: strings.Join(styles, " ")}
}

type Code struct {
	Language string
	Content  string
}

func CodeBlock(language, content string) Code {
	return Code{Language: language, Content: content}
}

func (c Code) String() string { return c.Content }
func (c Code) ANSI() string   { return c.Content }
func (c Code) HTML() string {
	return `<pre><code class="language-` + html.EscapeString(c.Language) + `">` + html.EscapeString(c.Content) + `</code></pre>`
}
func (c Code) Markdown() string { return "```" + c.Language + "\n" + c.Content + "\n```" }

type Collapsed struct {
	Label   string
	Content Textable
}

func (c Collapsed) String() string {
	return c.Label + ":\n" + c.Content.String()
}
func (c Collapsed) ANSI() string {
	return ansiText(c.Label+":", "font-bold") + "\n" + c.Content.ANSI()
}
func (c Collapsed) HTML() string {
	return "<details><summary>" + html.EscapeString(c.Label) + "</summary>" + c.Content.HTML() + "</details>"
}
func (c Collapsed) Markdown() string {
	return "**" + c.Label + ":**\n\n" + c.Content.Markdown()
}
