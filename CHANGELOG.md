# Changelog

## v0.1.1

- A card template may hold a `{{/* comment */}}` before its root element, or
  between a flex container's children. It was masked as a value, so startup
  refused the template with "text outside the root element".

## v0.1.0

The first release: card templates in an HTML and CSS subset, drawn in pure Go,
served at content-addressed URLs, a default card for every page, static export,
a development preview and a command to draw one card. See README.md and
DESIGN.md.
