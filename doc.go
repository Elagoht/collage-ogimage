// Package ogimage is a collage plugin that draws each page's share card — the image
// a link is shown with on X, LinkedIn, Slack and in messengers — from an HTML
// template, in pure Go, and serves it at a URL made from its content.
//
// A card is an html/template file under og/ in the template root, written in a
// documented subset of HTML and CSS. A page declares its card from its data
// handler:
//
//	if err := ogimage.Set(rc, "og/post.html", ogimage.Card{Title: post.Title, Label: post.Category}); err != nil {
//		return view{}, nil, err
//	}
//
// and the head gets og:image, its size and type, and a large Twitter card. A page
// that sets none gets Config.Default, drawn from its title and description.
//
// A template is written in two kinds of element: a flex container, which says
// display:flex and holds boxes, and a text block, which holds text and inline
// runs such as <b>. Inside that subset a card looks close to the same in a
// browser; the development preview at /_og/preview/ is the truth.
//
// A card's URL is a hash of what is drawn, so it is served immutable and never
// needs invalidating: a changed page has a new card at a new URL. It is drawn on
// the first request for it, never during the page's render, and only when a render
// of this site recorded it — a request cannot make the server draw a card it did
// not decide on.
//
// README.md is the guide; DESIGN.md the specification, and why it is as it is.
package ogimage
