// Package ogimage is a collage plugin that draws each page's share card — the image
// a link is shown with on X, LinkedIn, Slack and in messengers — from an HTML
// template, in pure Go, and serves it at a URL made from its content.
//
// A card is an html/template file under og/ in the template root, written in a
// documented subset of HTML and CSS. A page declares its card from its data
// handler:
//
//	ogimage.Set(rc, "og/post.html", ogimage.Card{Title: post.Title, Label: post.Category})
//
// and the head gets og:image, its size and type, and a large Twitter card. A page
// that sets none gets Config.Default, drawn from its title and description.
//
// A card's URL is a hash of what is drawn, so it is served immutable and never
// needs invalidating: a changed page has a new card at a new URL. It is drawn on
// the first request for it, never during the page's render, and only when a render
// of this site recorded it — a request cannot make the server draw a card it did
// not decide on.
//
// The package is in development. README.md documents v0.1 as designed and
// DESIGN.md is its specification; until v0.1.0 is released, Init refuses to start.
package ogimage
