package ogimage

import (
	"bytes"
	"strings"

	"golang.org/x/net/html"
)

// head is what the default card and the page's card are read from: a rendered
// page's title, description and og:image.
type head struct {
	title, description, image string
}

// readHead reads a rendered page's head with the HTML tokenizer — not by
// matching strings, since a minimizer may have dropped the quotes around
// attributes — stopping at </head>.
func readHead(page []byte) head {
	var h head
	z := html.NewTokenizer(bytes.NewReader(page))
	inTitle := false
	for {
		switch z.Next() {
		case html.ErrorToken:
			return h
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			switch string(name) {
			case "title":
				inTitle = true
			case "meta":
				var nameAttr, prop, content string
				for hasAttr {
					var k, v []byte
					k, v, hasAttr = z.TagAttr()
					switch string(k) {
					case "name":
						nameAttr = string(v)
					case "property":
						prop = string(v)
					case "content":
						content = string(v)
					}
				}
				switch {
				case strings.EqualFold(nameAttr, "description") && h.description == "":
					h.description = content
				case prop == "og:image" && h.image == "":
					h.image = content
				}
			case "body":
				return h
			}
		case html.TextToken:
			if inTitle && h.title == "" {
				h.title = strings.TrimSpace(string(z.Text()))
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			switch string(name) {
			case "title":
				inTitle = false
			case "head":
				return h
			}
		}
	}
}
