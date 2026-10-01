# elagoht/ogimage

A collage plugin that draws each page's share card — the image X, LinkedIn,
Slack and messengers show for a link — from a template you write in HTML and CSS,
in pure Go, and serves it at a URL made from its content.

> **Status: in development — phases 1 to 3 of 6 done.** This README describes
> v0.1 as designed; the specification is [DESIGN.md](DESIGN.md). The template
> parser and validator, the flexbox layout and the text engine exist and are
> tested against Chrome; drawing does not yet. Nothing is
> released, and `Init` refuses to start until it is.

```go
app, err := collage.New(&collage.Config{
	BaseURL: "https://example.com",
	Plugins: []collage.Plugin{ogimage.NewWith(ogimage.Config{
		Templates: templatesFS,
		Root:      "templates",
		Default:   "og/default.html",
	})},
})
```

Requires collage v0.40.0 or later. The site's `Config.BaseURL` is required:
networks only follow absolute `og:image` URLs.

- [Why](#why)
- [A card is a template](#a-card-is-a-template)
- [Setting a page's card](#setting-a-pages-card)
- [The default card](#the-default-card)
- [What HTML and CSS work](#what-html-and-css-work)
- [Text](#text)
- [Fonts](#fonts)
- [Images](#images)
- [URLs and caching](#urls-and-caching)
- [Static export](#static-export)
- [Designing a card](#designing-a-card)
- [Testing](#testing)
- [Security](#security)
- [Configuration](#configuration)
- [Limitations](#limitations)

## Why

Without a card of its own, a link is shown with the site's logo — usually a
square icon, which every network crops to a strip — or with nothing. With this
plugin every page has a 1200×630 card with its own title on it: a post's headline
in the site's type, its category and date; the about page's name and role; and,
with no code at all, every other page's title.

It is the idea of Next.js's `@vercel/og`, with three differences: the card is a
plain HTML template, not JSX; it is drawn in Go, with no browser and no binary to
install; and it is cached by its content, so it is drawn once and never
invalidated.

## A card is a template

A card is an HTML file under `og/` in your template root. The root element is the
image, 1200×630 unless it says otherwise:

```html
<!-- templates/og/post.html -->
<div style="display:flex; flex-direction:column; justify-content:space-between;
            width:1200px; height:630px; padding:72px;
            background:linear-gradient(135deg, #0f172a, #1e293b); color:#f8fafc;
            font-family:Outfit">
  <div style="display:flex; align-items:center; gap:16px">
    <img src="/static/icons/logo.png" style="width:56px; height:56px; border-radius:12px">
    <span style="font-size:28px; color:#94a3b8">{{.Site.Name}}</span>
  </div>

  <h1 data-fit style="font-size:72px; font-weight:800; line-height:1.1;
                      display:-webkit-box; -webkit-box-orient:vertical;
                      -webkit-line-clamp:3; overflow:hidden">{{.Title}}</h1>

  <div style="display:flex; gap:24px; font-size:28px; color:#94a3b8">
    <span>{{.Label}}</span>
    <span>{{.Fields.date}}</span>
  </div>
</div>
```

It is `html/template`: values are escaped, `{{if}}` and `{{range}}` work, and in
development the file is read from disk on every use, so an edit shows on the next
reload — when the application hands the plugin the template directory on disk in
development, as the scaffold does for its static files; an `embed.FS` never
changes. Styles are inline: there is no stylesheet. The CSS is a subset — see
[What HTML and CSS work](#what-html-and-css-work) — and a property outside it stops
the application at startup with the template, line and property named, rather than
drawing a card wrong.

A template sees:

| | |
| --- | --- |
| `.Title`, `.Description`, `.Label`, `.Image` | the page's `ogimage.Card` |
| `.Fields.<name>` | the card's extra values, by name |
| `.Site.Name`, `.Site.URL`, `.Site.Host` | the site, from `Config.SiteName` and `Config.BaseURL` |
| `.Page.Path`, `.Page.Locale` | the page the card is for |

## Setting a page's card

A page sets its card from the data handler that already has its content:

```go
func postData(ctx context.Context, rc *collage.RenderContext) (postView, []string, error) {
	post, err := client.Post(ctx, rc.Param("slug"))
	if err != nil {
		return postView{}, nil, err
	}
	if err := ogimage.Set(rc, "og/post.html", ogimage.Card{
		Title:  post.Title,
		Label:  post.Category,
		Image:  post.CoverURL,
		Fields: map[string]string{"date": post.Published.Format("2 January 2006")},
	}); err != nil {
		return postView{}, nil, err
	}
	return postView{Post: post}, []string{"post:" + post.Slug}, nil
}
```

`Set` returns an error — an unknown template, or a value the renderer cannot draw
— so a broken card fails where it was asked for, like any failure of the handler.
A handler that would rather serve the page without its card logs it and carries
on. Without the plugin registered (a test), `Set` does nothing.

The head gets:

```html
<meta property="og:image" content="https://example.com/_og/3f9a0be2…c1.png">
<meta property="og:image:width" content="1200">
<meta property="og:image:height" content="630">
<meta property="og:image:type" content="image/png">
<meta property="og:image:alt" content="The post's title">
<meta name="twitter:card" content="summary_large_image">
<meta name="twitter:image:alt" content="The post's title">
```

The layout places them with `{{hoist "head"}}`, as every hoisted tag.

`ogimage.Card` is the same five fields for every card:

| Field | For |
| --- | --- |
| `Title` | the headline |
| `Description` | a sentence under it |
| `Label` | a kicker: a category, a section, a tag |
| `Image` | a picture the template may place: a cover, an avatar |
| `Fields` | anything else, as strings, by name |

One shape for every card is what lets any card template serve as the default, and
a page move from the default to a card of its own by naming a template.

**With elagoht/meta.** Both write `og:image`, under the same key, and the later
declaration in a handler wins. Call `ogimage.Set` after `meta.Set` for the card to
be the image, before it for meta's `Image` to be. Register ogimage **after** meta in
`Config.Plugins`, and leave meta's `DefaultImage` unset; see
[the default card](#the-default-card).

## The default card

Name a template in `Config.Default` and every page that sets no card of its own
gets one, drawn from its `<title>` as `.Title` and its meta description as
`.Description`:

```html
<!-- templates/og/default.html -->
<div style="display:flex; flex-direction:column; justify-content:center; gap:24px;
            width:1200px; height:630px; padding:96px; background:#ffffff; color:#0f172a">
  <span style="font-size:32px; color:#64748b">{{.Site.Host}}</span>
  <h1 data-fit style="font-size:80px; font-weight:800; display:-webkit-box;
                      -webkit-box-orient:vertical; -webkit-line-clamp:2; overflow:hidden">{{.Title}}</h1>
  <p style="font-size:32px; color:#475569">{{.Description}}</p>
</div>
```

A page whose render declared an `og:image` — its own card, or a cover set through
elagoht/meta — keeps it. The default card is added after the render, into the head
the layout marked with `{{hoist "head"}}`, and the page cache stores the result, so
a cached page is not drawn again.

With elagoht/meta, two things: leave its `DefaultImage` unset, since a default image
is an `og:image` on every page and the default card would never apply; and register
ogimage after meta, so that its `twitter:card=summary_large_image` replaces meta's
`summary`.

## What HTML and CSS work

The renderer draws a fixed subset, with the meaning CSS gives it, over a default
stylesheet that sets every margin to 0 and every box to `border-box` (it is in
[DESIGN.md §6.6](DESIGN.md#66-the-default-stylesheet); paste it into a page to
sketch a card in a browser). Inside the subset a card looks close to the same in a
browser — but not pixel for pixel, and `data-fit` is the renderer's alone. **The
development preview is the truth.**

**Elements:** `div`, `section`, `header`, `footer`, `main`, `article`, `span`, `p`,
`h1`–`h6`, `strong`, `b`, `em`, `i`, `small`, `img`, `br`. Attributes: `style`,
`src`, `alt`, `data-fit`, and `class`, which is accepted and ignored.

**Every element is one of two kinds:**

- **a flex container**, which says `display:flex`, and lays its children out as
  boxes — there is no block flow and no float, so an element holding a `div`, an
  `img` or a heading and not saying `display:flex` is an error at startup;
- **a text block**, which says no `display` and holds only text and `span`,
  `strong`, `b`, `em`, `i`, `small`, `br` — a paragraph, wrapped across its runs,
  so `a <b>modular</b> monolith` is one sentence with a bold word.

| | |
| --- | --- |
| Layout | `display` (`flex`, `none`), `flex-direction`, `flex-wrap`, `justify-content`, `align-items`, `align-self`, `flex`, `flex-grow`, `flex-shrink`, `flex-basis`, `gap`, `row-gap`, `column-gap` |
| Size | `width`, `height`, `min-width`, `min-height`, `max-width`, `max-height`, `padding`, `margin` (with `auto`) — always `border-box` |
| Paint | `background`, `background-color`, `background-image` (colour, `linear-gradient()`, `url()` with `background-size: cover \| contain`), `color`, `border` (`solid`), `border-radius`, `opacity`, `overflow: hidden` |
| Text | `font-family`, `font-size`, `font-weight`, `font-style`, `line-height`, `letter-spacing`, `text-align`, `text-transform`, `white-space`, `-webkit-line-clamp`, `text-overflow` |
| Images | `object-fit` (`cover`, `contain`, `fill`) |

Lengths are `px`, `%`, `em`, `rem`. Colours are hex, `rgb()`, `rgba()`, `hsl()`,
`hsla()`, `transparent` and named colours.

Not supported, and an error at startup: `position`, `grid`, `float`, `transform`,
`box-shadow`, `filter`, `text-shadow`, `::before`/`::after`, `<style>`, `<svg>`,
and every element and property not listed above.

## Text

Text wraps at spaces and after hyphens. Three things a card needs and a page
rarely does:

- **Clamping** stops after N lines and ends the last with `…`. Write it the way a
  browser needs it, all four together:
  `display:-webkit-box; -webkit-box-orient:vertical; -webkit-line-clamp:3; overflow:hidden`.
  `-webkit-line-clamp` alone would be ignored by a browser, so it is an error.
- **`data-fit`** shrinks the font, 2px at a time down to half its size, until the
  text fits its box and its clamp — for a title that may be four words or forty. A
  browser ignores the attribute and shows the declared size.
- **`text-transform: uppercase`** is Unicode-aware, and knows Turkish: `i` becomes
  `İ` in a card for a `tr` page.

Latin (Turkish included), Greek and Cyrillic are drawn. Right-to-left and complex
scripts, and emoji, are not in v0.1.

## Fonts

```go
Fonts: []ogimage.Font{
	{Family: "Outfit", Weight: 400, File: "static/fonts/Outfit-Regular.ttf"},
	{Family: "Outfit", Weight: 800, File: "static/fonts/Outfit-ExtraBold.ttf"},
	{Family: "Outfit", Weight: 400, Style: "italic", File: "static/fonts/Outfit-Italic.ttf"},
},
FontFiles: staticFS,
```

TrueType and OpenType files, read from `FontFiles` when the application starts. A
weight that is not registered uses the nearest one of its family. With no fonts at
all, cards are drawn in the Go fonts (`gofont`), which are bundled — a card always
draws. A character no registered font has falls back to the Go fonts, and one no
font has is drawn as `□` and logged.

Register the fonts your cards use; a webfont the page loads from Google Fonts is
not something the renderer can see.

## Images

An `<img src>` or a `background: url()` can be:

- **a file of the site**, under a prefix in `Config.Files` — `/static/logo.png`
  read straight from the mapped `fs.FS`, with no request made;
- **a remote image** whose origin is in `Config.ImageOrigins` — a CMS cover, say —
  fetched when the card is drawn, with a timeout and a size cap;
- **a `data:` URL** of a PNG, JPEG or WebP.

PNG, JPEG, GIF (first frame) and WebP are read. `object-fit` places them; a
`border-radius` and `overflow: hidden` round them.

## URLs and caching

A card's URL is a hash of what is drawn — the executed template, the image size,
the fonts and the renderer's version:

```
/_og/3f9a0be24c71d6aa95e3b8c0d1f2e4c1.png
```

So:

- **It is served `immutable`, for a year.** Networks and CDNs keep it for good.
- **Nothing needs invalidating.** A post whose title changes renders a new card at
  a new URL; the post's page — invalidated the way it always is, by a webhook or a
  TTL — carries the new `og:image`. Two pages whose cards come out the same share
  one image.
- **It is drawn on the first request for it**, not while the page renders. A page
  is never slower for its card, and a card nobody shares is never drawn.

The card follows its page's strategy: a `Static()` page's card is recorded once, an
`Incremental()` page's each time it renders again — the same URL while the content
is the same — and a `Dynamic()` page's on each render, which for unchanged content
is a lookup.

**Set `Dir` when the page cache is on disk.** What a card is made of is kept so it
can be drawn when asked for. Kept only in memory, it is gone after a restart, while
a page cached on disk before the restart still names it — and that card is a 404.
With `Dir`, cards and what they are made of survive restarts. The plugin warns at
startup when `Dir` is empty outside development.

## Static export

`collage export` writes every card its pages recorded to `_og/<hash>.png` in the
output, and a card that fails to draw fails the build.

## Designing a card

- **Preview.** In development, `/_og/preview/` lists the cards the running site
  recorded, newest first, beside their pages, and reloads when a card template
  changes. Open a page, then the preview.
- **Draw one without the site.** `go run . ogimage og/post.html card.json > card.png`
  draws a template with the `Card` in `card.json` (`{"title": "…", "label": "…"}`).
- **Check in a browser.** Open the template's HTML in a browser with sample text in
  place of `{{…}}`: inside the subset, it looks the same.

## Testing

```go
img, err := ogimage.Draw(cfg, `<div style="width:1200px;height:630px;background:#000"></div>`)
```

draws card HTML directly, for golden-image tests, and

```go
og := ogimage.NewWith(cfg)        // the plugin the test's application is built with
// ... request /blogs/hello-world
card, ok := og.Recorded("/blogs/hello-world")
```

returns the card a page recorded on its last render — its template, its `Card`,
its HTML and its URL — for a test that a page sets the card it should.

## Security

Drawing a card costs CPU, so nothing a request carries can make the server draw
one it did not decide on:

- **A URL is only a hash.** A card is drawn only when a render of this site recorded
  it; any other hash is a 404, with nothing drawn. No query, header or path segment
  is input to a card.
- **Don't put request input in a card.** A search page whose card shows the search
  term records a card per term anyone types. The store's caps bound that
  (`MaxEntries`, `MaxBytes`), and in development the plugin warns when one page
  records more than 100 cards — but the fix is a card without the term.
- **Drawing is bounded:** at most `Workers` at once, each abandoned after
  `RenderTimeout`; concurrent requests for one card share one draw.
- **Images are bounded:** remote ones only from `ImageOrigins`, with a 5 s timeout,
  a 10 MB cap and no redirect to another origin, decoded only under 40 megapixels;
  local ones only through `Files`.
- **Only PNG is served** from the prefix — never HTML or SVG a browser would run.

## Configuration

| Field | Default | |
| --- | --- | --- |
| `Templates` | required | the `fs.FS` card templates are read from — usually the application's own |
| `Root` | `"templates"` | the directory in `Templates` names are relative to |
| `CardDir` | `"og"` | the directory under `Root` holding card templates |
| `Default` | none | the template for every page that sets no card |
| `SiteName` | the host of `BaseURL` | `.Site.Name` |
| `Files` | none | URL prefix → `fs.FS` that local images are read from: `{"/static/": staticFS}` |
| `ImageOrigins` | none | origins remote images may be fetched from: `"https://cms.example.com"` |
| `Fonts`, `FontFiles` | Go fonts | the fonts cards are drawn in, and the `fs.FS` their files are in |
| `Dir` | memory only | where cards and their specs are kept across restarts |
| `Prefix` | `"/_og/"` | the URL prefix cards are served under |
| `MaxEntries` | `10000` | how many cards are kept; negative for unlimited |
| `MaxBytes` | `256 MiB` | how many bytes of cards are kept; negative for unlimited |
| `Workers` | half the CPUs, at least 1 | cards drawn at once |
| `RenderTimeout` | `5s` | how long one card may take to draw |

Everything but the `fs.FS` fields can also come from the plugin configuration:

```json
{
  "elagoht/ogimage": {
    "default": "og/default.html",
    "siteName": "Example",
    "imageOrigins": ["https://cms.example.com"],
    "dir": ".cache/ogimage",
    "maxEntries": 10000
  }
}
```

## Limitations

- A subset of CSS, not all of it; there is no browser underneath. A browser-backed
  renderer that takes the same templates is planned as a separate module.
- No right-to-left or complex scripts, no emoji, no SVG images in v0.1.
- A remote image behind an unchanged URL is drawn as it was the first time; a CMS
  that versions its URLs avoids this, as it does for every `<img>` on a page.
- The default card reads the title and description from the rendered head, so a
  page that writes them some other way than `<title>` and
  `<meta name="description">` gets an empty one.
