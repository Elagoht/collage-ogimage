package ogimage

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Elagoht/collage-ogimage/internal/css"
	"github.com/Elagoht/collage-ogimage/internal/dom"
)

// ErrUnsupported is wrapped by every error about markup or CSS the renderer
// cannot draw. Each such error is a *TemplateError naming where it is.
var ErrUnsupported = errors.New("ogimage: the renderer cannot draw this")

// TemplateError is one thing in a card template the renderer cannot draw.
type TemplateError struct {
	// Template is the card template's name: "og/post.html".
	Template string
	// Line and Col are where in the template the problem is, 1-based, Col
	// counting characters — the position an editor shows.
	Line, Col int
	// Msg says what is wrong, and what to write instead where there is an
	// answer.
	Msg string
}

func (e *TemplateError) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s", e.Template, e.Line, e.Col, e.Msg)
}

// Unwrap makes every TemplateError an ErrUnsupported.
func (e *TemplateError) Unwrap() error { return ErrUnsupported }

// checkTemplate validates a card template's markup as written, before it ever
// runs: its elements, attributes and styles, and the two kinds of element. Each
// template action is masked first — replaced, character for character, by
// css.Masked — so every position reported is the template's own, and a style
// value written as an action is left to be checked when the template runs.
//
// It returns nil, or every problem joined, each a *TemplateError.
func checkTemplate(name, src string) error {
	_, errs := dom.Parse(mask(src))
	if len(errs) == 0 {
		return nil
	}
	out := make([]error, len(errs))
	for i, e := range errs {
		out[i] = &TemplateError{Template: name, Line: e.Line, Col: e.Col, Msg: e.Msg}
	}
	return errors.Join(out...)
}

// mask replaces every template action in src — "{{.Title}}", "{{if .X}}",
// "{{/* a comment */}}" — with as many css.Masked characters as the action has
// characters, keeping its newlines, so the result has the template's lines and
// columns. An action is found the way text/template finds one: from "{{" to the
// first "}}" outside a quoted string or a raw string.
func mask(src string) string {
	var b strings.Builder
	b.Grow(len(src))
	i := 0
	for i < len(src) {
		start := strings.Index(src[i:], "{{")
		if start < 0 {
			b.WriteString(src[i:])
			break
		}
		start += i
		b.WriteString(src[i:start])
		end := actionEnd(src, start+2)
		for _, r := range src[start:end] {
			if r == '\n' {
				b.WriteRune('\n')
			} else {
				b.WriteRune(css.Masked)
			}
		}
		i = end
	}
	return b.String()
}

// actionEnd returns the offset just past the "}}" that closes the action whose
// body begins at from, or len(src) when it is not closed — which html/template
// reports on its own when it parses the file.
func actionEnd(src string, from int) int {
	if strings.HasPrefix(src[from:], "/*") || strings.HasPrefix(src[from:], "- /*") {
		if end := strings.Index(src[from:], "*/"); end >= 0 {
			if close := strings.Index(src[from+end:], "}}"); close >= 0 {
				return from + end + close + 2
			}
		}
		return len(src)
	}
	var quote byte
	for j := from; j < len(src); j++ {
		c := src[j]
		switch {
		case quote != 0:
			if c == '\\' && quote != '`' {
				j++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'' || c == '`':
			quote = c
		case c == '}' && j+1 < len(src) && src[j+1] == '}':
			return j + 2
		}
	}
	return len(src)
}
