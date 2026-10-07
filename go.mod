// A collage plugin that draws each page's share card from an HTML template, in pure
// Go, and serves it at a URL made from its content.
//
// It requires collage the way any consumer does, and reaches nothing the framework
// does not offer every plugin.
module github.com/Elagoht/collage-ogimage

go 1.26.0

require github.com/Elagoht/collage v0.50.0

require (
	github.com/Elagoht/collage-meta v0.2.2
	golang.org/x/net v0.59.0
)

require golang.org/x/sys v0.48.0 // indirect

require (
	golang.org/x/image v0.46.0
	golang.org/x/text v0.42.0 // indirect
)
