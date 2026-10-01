package css

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// kind is what a token is.
type kind uint8

const (
	ident kind = iota
	number
	hash
	str
	function
	comma
	slash
)

// token is one component of a value: an identifier, a number with its unit, a
// #hex, a quoted string, a function with its comma-separated arguments, or a
// separator.
type token struct {
	kind kind
	// text is an identifier lower-cased, a string's contents, a hash's digits, or
	// a function's name lower-cased.
	text string
	// n and unit are a number's value and unit ("", "px", "%", "deg", ...).
	n    float64
	unit string
	// args are a function's arguments, split at the commas between them. A
	// url() holds its raw contents in text instead.
	args [][]token
}

// tokenize splits a value into tokens. Whitespace separates tokens and is not
// kept; commas and slashes are tokens of their own.
func tokenize(s string) ([]token, error) {
	var out []token
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f':
			i++
		case c == ',':
			out = append(out, token{kind: comma})
			i++
		case c == '/':
			out = append(out, token{kind: slash})
			i++
		case c == '"' || c == '\'':
			end := strings.IndexByte(s[i+1:], c)
			if end < 0 {
				return nil, fmt.Errorf("a string is not closed")
			}
			out = append(out, token{kind: str, text: s[i+1 : i+1+end]})
			i += end + 2
		case c == '#':
			j := i + 1
			for j < len(s) && isHex(s[j]) {
				j++
			}
			if j == i+1 {
				return nil, fmt.Errorf("# is not followed by a colour")
			}
			out = append(out, token{kind: hash, text: strings.ToLower(s[i+1 : j])})
			i = j
		case isDigit(c) || c == '.' || ((c == '-' || c == '+') && i+1 < len(s) && (isDigit(s[i+1]) || s[i+1] == '.')):
			j := i + 1
			for j < len(s) && (isDigit(s[j]) || s[j] == '.' || ((s[j] == 'e' || s[j] == 'E') && j+1 < len(s) && isDigit(s[j+1]))) {
				j++
			}
			n, err := strconv.ParseFloat(s[i:j], 64)
			if err != nil {
				return nil, fmt.Errorf("%q is not a number", s[i:j])
			}
			k := j
			for k < len(s) && (isLetter(s[k]) || s[k] == '%') {
				k++
			}
			out = append(out, token{kind: number, n: n, unit: strings.ToLower(s[j:k])})
			i = k
		case isNameStart(s, i):
			j := i
			for j < len(s) && isNameChar(s, j) {
				_, size := utf8.DecodeRuneInString(s[j:])
				j += size
			}
			name := strings.ToLower(s[i:j])
			if j < len(s) && s[j] == '(' {
				close, err := matching(s, j)
				if err != nil {
					return nil, fmt.Errorf("%s(: %w", name, err)
				}
				inner := s[j+1 : close]
				if name == "url" {
					out = append(out, token{kind: function, text: name, args: [][]token{{{kind: str, text: unquote(strings.TrimSpace(inner))}}}})
				} else {
					args, err := splitArgs(inner)
					if err != nil {
						return nil, fmt.Errorf("%s(): %w", name, err)
					}
					out = append(out, token{kind: function, text: name, args: args})
				}
				i = close + 1
			} else {
				out = append(out, token{kind: ident, text: name})
				i = j
			}
		default:
			r, _ := utf8.DecodeRuneInString(s[i:])
			return nil, fmt.Errorf("%q is not something a value can hold", r)
		}
	}
	return out, nil
}

// splitArgs tokenizes a function's contents and splits them at top-level commas.
func splitArgs(s string) ([][]token, error) {
	toks, err := tokenize(s)
	if err != nil {
		return nil, err
	}
	args := [][]token{{}}
	for _, t := range toks {
		if t.kind == comma {
			args = append(args, []token{})
			continue
		}
		args[len(args)-1] = append(args[len(args)-1], t)
	}
	return args, nil
}

// matching returns the index of the ')' that closes the '(' at open.
func matching(s string, open int) (int, error) {
	depth := 0
	var quote byte
	for i := open; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth == 0 {
				return i, nil
			}
		}
	}
	return 0, fmt.Errorf("a parenthesis is not closed")
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

func isDigit(c byte) bool  { return c >= '0' && c <= '9' }
func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isHex(c byte) bool    { return isDigit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }

func isNameStart(s string, i int) bool {
	c := s[i]
	if c == '-' {
		return i+1 < len(s) && (isLetter(s[i+1]) || s[i+1] == '-' || s[i+1] == '_')
	}
	return isLetter(c) || c == '_' || c >= utf8.RuneSelf
}

func isNameChar(s string, i int) bool {
	c := s[i]
	return isLetter(c) || isDigit(c) || c == '-' || c == '_' || c >= utf8.RuneSelf
}
