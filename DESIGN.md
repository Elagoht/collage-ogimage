# elagoht/ogimage — design

Status: **design, v0.1 in development.** This document is the specification the
implementation is written against. Where the code and this document disagree, one
of them is a bug; decide which, and fix both.

## 1. The problem

A page shared on X, LinkedIn, Slack, WhatsApp or iMessage is shown as a card: an
image, a title, a line of text. The image comes from the page's `og:image`. Most
sites have one of two answers, both bad:

- **No image, or the logo.** A 512×512 icon, cropped by every network to a 1.91:1
  strip. Every link from the site looks the same.
- **A designer exports one per page.** It works for ten pages and stops at the
  blog, where a new post has no card until someone makes it.

The answer Next.js made popular (`@vercel/og`) is to draw the card from the page's
own data: the post's title in the site's type, its category, its date. This plugin
is that, for collage: the card is a template, written in HTML and CSS, rendered to a
PNG in pure Go, and cached by the same rules as the page it belongs to.

## 2. Goals

1. **A card is a template.** The developer writes HTML and CSS in a file under the
   template root, with `html/template`'s `{{.Title}}`, and sees it as an image.
   Nothing to learn but which CSS works (§6).
2. **One line per page.** A page declares its card where it already declares its
   title: `ogimage.Set(rc, "og/post.html", ogimage.Card{...})`. The head tags are
   written for it.
3. **A card for every page, with no line at all.** A default template draws every
   page that does not set its own, from the page's title and description.
4. **Pure Go.** No browser, no cgo, no binary to install. The site's one binary
   still runs anywhere `go build` targets.
5. **Cached like the page.** A card is produced once per distinct content,
   addressed by that content, served immutable, invalidated by nothing because it
   never changes — a changed page has a new card at a new URL. A static export
   writes the cards.
6. **Safe to expose.** Producing a card costs CPU. Nothing a request carries can
   make the server produce one it did not already decide to.
7. **Loud when wrong.** CSS the renderer does not support is an error naming the
   template, line and property — at startup where it can be seen, never a card
   silently drawn wrong.

### Non-goals for v0.1

- Arbitrary HTML and CSS. The subset (§6) is fixed and small; a renderer that
  takes everything is a browser (§13).
- Complex text shaping: right-to-left scripts, Indic scripts, ligature-dependent
  scripts. Latin, Latin Extended (Turkish included), Greek and Cyrillic are drawn.
- Emoji, SVG images, box shadows, filters, transforms, absolute positioning, grid.
- Animated or vector output. A card is a PNG.

## 3. What a developer writes

### 3.1 The card template

`templates/og/post.html`:

```html
<div style="display:flex; flex-direction:column; justify-content:space-between;
            width:1200px; height:630px; padding:72px;
            background:linear-gradient(135deg, #0f172a, #1e293b); color:#f8fafc;
            font-family:Outfit">
  <div style="display:flex; align-items:center; gap:16px">
    <img src="/static/icons/android-chrome-192x192.png"
         style="width:56px; height:56px; border-radius:12px">
    <span style="font-size:28px; color:#94a3b8">{{.Site.Name}}</span>
  </div>

  <h1 data-fit style="font-size:72px; font-weight:800; line-height:1.1;
                      -webkit-line-clamp:3">{{.Title}}</h1>

  <div style="display:flex; gap:24px; font-size:28px; color:#94a3b8">
    <span>{{.Label}}</span>
    <span>{{.Fields.date}}</span>
  </div>
</div>
```

The root element's `width` and `height` are the image's size. 1200×630 is what
every network expects, and what a root that sets neither gets.

### 3.2 The page

```go
func (b *Blog) postData(ctx context.Context, rc *collage.RenderContext) (postView, []string, error) {
	post, err := b.Client.Post(ctx, rc.Param("slug"))
	if err != nil {
		return postView{}, nil, err
	}
	ogimage.Set(rc, "og/post.html", ogimage.Card{
		Title:  post.Title,
		Label:  post.Category.Name,
		Image:  b.Client.Asset(post.CoverImage),
		Fields: map[string]string{"date": post.PublishedAt.Format("2 January 2006")},
	})
	return view, tags, nil
}
```

### 3.3 The application

```go
ogimage.NewWith(ogimage.Config{
	Templates: templatesFS,              // the same fs.FS the application parses
	Root:      "templates",
	Default:   "og/default.html",        // every page that sets no card
	Files:     map[string]fs.FS{"/static/": staticFS},
	FontFiles: staticFS,
	Fonts: []ogimage.Font{
		{Family: "Outfit", Weight: 400, File: "static/fonts/Outfit-Regular.ttf"},
		{Family: "Outfit", Weight: 800, File: "static/fonts/Outfit-ExtraBold.ttf"},
	},
	Dir: filepath.Join(cacheDir, "ogimage"),
})
```

## 4. The card's data: `ogimage.Card`

```go
type Card struct {
	Title       string            `json:"title"`       // the headline
	Description string            `json:"description"` // a sentence under it
	Label       string            `json:"label"`       // a kicker: a category, a section
	Image       string            `json:"image"`       // a picture the template may place
	Fields      map[string]string `json:"fields"`      // anything else: {{.Fields.date}}
}
```

A template is executed with:

```go
type cardData struct {
	Card               // promoted: {{.Title}}, {{.Fields.date}}
	Site SiteInfo      // {{.Site.Name}} (Config.SiteName), {{.Site.URL}}, {{.Site.Host}}
	Page PageInfo      // {{.Page.Path}}, {{.Page.Locale}}
}
```

**Why a fixed struct and not "any data".** Three reasons, in order of weight:

1. **One shape makes templates portable.** The default card (§5.3) is drawn from a
   page that set nothing: the plugin fills a `Card` from the page's `<title>` and
   description. If templates took arbitrary data, the default template would need
   a different shape from every page's own. With one shape, any card template can
   be the default, and a site can move a page from the default to its own card by
   naming a template, not by rewriting one.
2. **Typed all the way.** No `any`, no reflection on the caller's types, no
   template that fails on the first render because a field is called `Headline`
   on one page and `Title` on another.
3. **It is what a card holds.** Cards are a title, a line, a label, a picture and
   a date in practice; `Fields` takes the rest as strings, which is what a template
   prints anyway.

## 5. How it works

### 5.1 The pipeline

```
 page render                                   GET /_og/<hash>.png
 ──────────                                    ───────────────────
 data handler                                  mount's fs.FS Open
   ogimage.Set(rc, tmpl, card)                   ├─ PNG on disk?  → serve
     ├─ execute tmpl with card → HTML            ├─ spec known?   → render → store → serve
     ├─ parse HTML → box tree, validate          └─ neither       → 404
     ├─ hash(engine, size, fonts, HTML)
     ├─ record spec  (memory, Dir)
     └─ hoist og:image=/_og/<hash>.png
           og:image:width, :height, :type
           twitter:card=summary_large_image
```

**The template is executed at render time, into the final HTML of the card**, and
that HTML is what is hashed and stored. Rendering the PNG later needs nothing from
the page, the request or the template — only the stored HTML and the fonts. This
is what makes the URL content-addressed (§7), lets a restarted process serve a card
a page cached on disk before the restart points at (§8), and keeps the expensive
step out of the page's render.

### 5.2 Declaring: `ogimage.Set`

`Set(rc, template, card)`:

1. Looks `template` up among the card templates parsed at startup. An unknown name
   is an error, reported the way a template error inside a data handler is: in
   development on the page's error panel; in production logged, and the page keeps
   whatever `og:image` it had.
2. Executes it with `cardData`. Every value is escaped by `html/template`, as on a
   page.
3. Parses the HTML into the box tree (§6) and validates every element, attribute
   and style property. Static parts of each template were already validated at
   startup (§10); this catches values that arrived in the data, such as a colour.
4. Computes the hash (§7), records the spec, and hoists the tags (§5.4).

`Set` called twice in one render: the later call wins, as any hoist of the same key
from the same fragment does in collage, and a deeper fragment's wins over its
parent's. Only the winning card is recorded.

### 5.3 The default card

A page that calls `Set` nowhere gets `Config.Default`, when it is set, drawn from:

- `Title`: the page's `<title>`;
- `Description`: its `<meta name="description">`;
- `Label`, `Image`, `Fields`: empty.

These exist only once the page has rendered, so the default card is applied in
`OnAfterRender`: the plugin reads the title and description from the head of the
rendered HTML, renders the default template with them, and writes the `og:image`
tags into the head — **unless the head already carries an `og:image`**. A page with
a cover of its own (`meta.Set(rc, meta.Page{Image: ...})`) keeps it; a page with no
image gets a card. With elagoht/meta, leave its `DefaultImage` unset: a default
image there is an `og:image` on every page, and the default card would never apply.

The rewritten HTML is what the page cache stores, so the default card is computed
once per cached page, like the page.

### 5.4 The head tags

The plugin hoists, under the same keys collage's `HoistProperty` and `HoistMeta` —
and so elagoht/meta — use:

```html
<meta property="og:image" content="https://example.com/_og/3f9a…c1.png">
<meta property="og:image:width" content="1200">
<meta property="og:image:height" content="630">
<meta property="og:image:type" content="image/png">
<meta property="og:image:alt" content="{{Title}}">
<meta name="twitter:card" content="summary_large_image">
<meta name="twitter:image:alt" content="{{Title}}">
```

The URL is absolute against `Host.BaseURL()` (collage v0.39.0), as networks
require; the application does not start without one. Called after `meta.Set` in the
same handler, `ogimage.Set` replaces meta's `og:image`; called before, meta's wins.
The order is the developer's decision and is documented as such.

## 6. The HTML and CSS subset

The renderer implements a documented subset of HTML and CSS, with the meaning CSS
gives it. A card that stays inside the subset renders the same in a browser — which
is the promise that lets a browser-backed renderer (§13) take the same templates.

### 6.1 Elements

| Element | Meaning |
| --- | --- |
| `div`, `section`, `header`, `footer`, `main`, `article` | a box |
| `span`, `p`, `h1`–`h6`, `strong`, `em`, `b`, `i`, `small` | a box holding text; `strong`/`b` default to weight 700, `em`/`i` to italic, `h1`–`h6` to a bold weight and the font sizes of the default stylesheet |
| `img` | a replaced box, `src` required |
| `br` | a line break inside text |

Any other element is an error. Attributes other than `style`, `src`, `alt`,
`data-fit` and `class` are errors; `class` is accepted and ignored, so markup
copied from a page does not fail on it.

### 6.2 Layout

Every box is a flex container. `display` is `flex` (the default) or `none`; there is
no block or inline flow, so a box's children are laid out on its main axis.

| Property | Values |
| --- | --- |
| `flex-direction` | `row` (default), `column` |
| `justify-content` | `flex-start`, `flex-end`, `center`, `space-between`, `space-around`, `space-evenly` |
| `align-items`, `align-self` | `flex-start`, `flex-end`, `center`, `stretch` |
| `flex-grow`, `flex-shrink` | numbers |
| `flex-basis` | length, `auto` |
| `flex-wrap` | `nowrap` (default), `wrap` |
| `gap`, `row-gap`, `column-gap` | length |
| `width`, `height`, `min-*`, `max-*` | length, percentage, `auto` |
| `padding`, `margin` (and `-top` etc.) | length; `margin: auto` centres |
| `box-sizing` | `border-box` only, the default |

Lengths are `px`, `%`, `em` and `rem` (`rem` against the root's font size). The
layout follows the CSS Flexible Box Layout algorithm for these properties; where
the algorithm has a choice the subset does not expose, it takes the CSS default.

### 6.3 Painting

| Property | Values |
| --- | --- |
| `background`, `background-color` | a colour; `linear-gradient(angle, stops…)`; `url(...)` with `background-size: cover \| contain` |
| `color` | a colour |
| `border`, `border-width`, `border-color`, `border-style` | `solid` only |
| `border-radius` | length, percentage; four corners |
| `opacity` | 0–1 |
| `overflow` | `visible`, `hidden` (clips to the border radius) |

Colours are `#rgb`, `#rrggbb`, `#rrggbbaa`, `rgb()`, `rgba()`, `hsl()`, `hsla()`,
`transparent` and the CSS named colours.

### 6.4 Text

| Property | Values |
| --- | --- |
| `font-family` | a family registered in `Config.Fonts`, or `sans-serif`, `serif`, `monospace` |
| `font-size` | length |
| `font-weight` | 100–900, `normal`, `bold` |
| `font-style` | `normal`, `italic` |
| `line-height` | number, length |
| `letter-spacing` | length |
| `text-align` | `left`, `center`, `right` |
| `text-transform` | `none`, `uppercase`, `lowercase` (Unicode-aware: `i` → `İ` in a `tr` card) |
| `white-space` | `normal`, `nowrap`, `pre-wrap` |
| `-webkit-line-clamp`, `line-clamp` | a number of lines, ellipsis after the last |
| `text-overflow` | `ellipsis` with `white-space: nowrap` |

Text wraps at spaces and at the break opportunities of Unicode line breaking
(UAX #14) a card meets in practice: after hyphens, and between CJK ideographs.

**`data-fit`** is the one extension, an attribute rather than a property so a
browser ignores it: the text's font size is reduced, in 2px steps, until it fits
its box within its line clamp, down to half the declared size. It is what a title
needs when it may be four words or forty.

A font family, weight and style not registered falls back to the nearest
registered weight of the family, then to the bundled Go fonts (`gofont`). A
character no registered font has is drawn from the fallback, and a character no
font has is drawn as `□` — and logged once per template, at startup when the
character is in the template, at render when it came in the data.

### 6.5 Images

`img` and `background: url()` take:

- a path under a prefix in `Config.Files` (`/static/icon.png` read from the mapped
  `fs.FS` — no request is made to the application itself);
- an absolute `http(s)` URL whose origin is in `Config.ImageOrigins`, fetched at
  render time with a timeout and a size cap (§9);
- a `data:` URL of a PNG, JPEG or WebP.

`object-fit: cover | contain | fill` and `object-position: center` (other positions
are v0.2). Formats are PNG, JPEG, GIF (first frame) and WebP.

## 7. Content addressing

```
hash = SHA-256(engineVersion ‖ width ‖ height ‖ fontSetDigest ‖ cardHTML)[:16]
URL  = Prefix + hex(hash) + ".png"          e.g. /_og/3f9a0b…e2c1.png
```

- **`cardHTML`** is the executed template, after `html/template` — the exact input
  the renderer draws. Two pages whose cards come out the same share one image.
- **`engineVersion`** changes when a release of the plugin draws the same HTML
  differently. Upgrading the plugin moves every card to a new URL, so no network
  keeps a card drawn by the old engine under a name the new one owns.
- **`fontSetDigest`** is a hash of the registered font files' bytes. Changing a
  font changes every card.
- **Remote images are not in the hash**, only their URLs (inside the HTML). A CMS
  that replaces the bytes behind an unchanged URL is not seen until the card is
  drawn again; the same is true of every `<img>` on every page, and the CMS's own
  versioned URLs are the answer.

Because the URL is the content, a card is served `Cache-Control: public,
max-age=31536000, immutable`, and **there is nothing to invalidate**: a changed
page has a new card at a new URL, and the page's own invalidation — a webhook, a
TTL — is what moves its `og:image`.

## 8. Storage and lifecycle

### 8.1 What is stored

| Item | Where | Bounded by |
| --- | --- | --- |
| spec: the card's HTML and size | memory index, and `Dir/specs/<hash>.html` | `MaxEntries`, `MaxBytes` |
| image: the PNG | `Dir/png/<hash>.png` | `MaxBytes` |

With `Dir` empty both live in memory only. That is right for development and for a
memory page cache. **With a disk page cache, set `Dir`**: a page cached on disk
before a restart points at `/_og/<hash>.png`, and only a spec on disk lets the
restarted process draw it. The plugin warns at startup, outside development, when
`Dir` is empty.

Eviction is oldest-first, by the time a spec was last recorded or an image last
served, past either cap. An evicted image is drawn again from its spec when asked
for; an evicted spec is a 404 for a URL some page may still carry — so the caps
should be well above the number of live cards, and the defaults (10 000 specs,
256 MiB) are.

### 8.2 Following the page's strategy

The card is recorded when its page renders, so it follows the page:

| Page | Card |
| --- | --- |
| `Static()` | recorded at the one render; drawn on the first request for it; exported |
| `Incremental(ttl)` | recorded each time the page is rendered again — the same hash while the content is the same, a new one when it changed |
| `Dynamic()` | recorded on every render, which is a hash lookup when the content repeats; a dynamic page whose card varies per request records a spec per variation (§9) |
| `Shared` fragment | whatever the page's render is |

Drawing happens on the first `GET` of the image, not in the page's render: a page
is never slower because of its card, and a card nobody asks for is never drawn. A
static export draws every recorded card (§8.3).

### 8.3 Static export

The plugin implements `BuildFinishedHook`: once every page is written, it draws
each card the build's renders recorded and writes it to `OutDir/_og/<hash>.png`,
reporting a card that failed to draw with `ev.Error`, which fails the build. The
mount is registered `WithoutBuildCopy`, since the mount's filesystem cannot list
cards no render recorded yet.

### 8.4 Development

In development nothing is cached: every `GET /_og/<hash>.png` draws the card again,
and templates are read from disk on every `Set`, so an edited template shows up on
the next page reload and the next image request. `/_og/preview/` (development only)
lists the cards this process recorded, newest first, beside the page that recorded
each, and reloads itself when a card template changes.

## 9. Security

The plugin produces images on request, which costs CPU and memory. The model:

1. **Only the server decides what is drawn.** A URL names a hash; a hash is drawn
   only when a render of this application recorded its spec. A hash nobody
   recorded is a 404 without any drawing. No query parameter, header or path
   segment is input to a card.
2. **A card's content is the page's.** The data in `ogimage.Set` comes from the
   data handler. A handler that puts request input in a card — a search term on a
   search page — lets a client mint one spec per distinct input. The caps bound
   it (§8.1); the documentation says not to, and in development the plugin warns
   when one page records more than 100 distinct cards.
3. **Drawing is bounded.** At most `Workers` cards are drawn at once (default:
   half the CPUs, at least one); one draw is abandoned after `RenderTimeout`
   (default 5 s). Concurrent requests for one card share one draw.
4. **Images are bounded.** A remote image is fetched only from
   `Config.ImageOrigins`, with a 5 s timeout, a 10 MB body cap, no redirects to
   another origin, and decoded only below 40 megapixels. A local image is read only
   from `Config.Files`, through an `fs.FS`, never a path joined to a directory.
5. **Templates are the application's.** Card templates are files the application
   ships; nothing a request carries names a template.
6. **The output is a PNG.** No HTML or SVG is served from the prefix, so a card is
   never a document a browser runs.

## 10. Startup and validation

`Init`:

1. Requires `Host.BaseURL()` (absolute `og:image` URLs) — `ErrNoBaseURL`.
2. Parses every card template: each `.html` file under `CardDir` (default `og`)
   inside `Root` of `Templates`, with `html/template`. A name passed to `Set` is
   one of these files' paths relative to `Root` (`og/post.html`), and nothing
   else: card templates are not looked up among the page templates.
3. Validates each template's **static** markup: elements, attributes and style
   properties written literally. An unsupported one is `ErrUnsupported`, wrapped
   with the template, line and column, and the property or element — the
   application does not start.
4. Loads and parses every font in `Config.Fonts`; a file that is missing or not a
   TrueType/OpenType font is an error.
5. Mounts `Prefix` (default `/_og/`) and, in development, `Prefix + "preview/"`.
6. Registers the `ogimage` command (§11).

## 11. Tooling

- **Command:** `go run . ogimage <template> [card.json] > card.png` draws a card
  template with a `Card` decoded from the JSON file (or `{}`), without starting a
  server. It is the loop for designing a card, and the way to keep golden images
  in a repository.
- **Testing:** `ogimage.Draw(cfg, html) (image.Image, error)` draws card HTML
  directly, for golden tests; `ogimage.Recorded(app, path)` returns the card a
  page recorded on its last render, for asserting a page sets the card it should.
- **`collage check`**: v0.2 contributes the card templates' validation to it.

## 12. Implementation

### 12.1 Packages

```
ogimage.go        Plugin, Config, New/NewWith, Init, hooks, Set
card.go           Card, cardData, template execution
store.go          spec and image store: memory index, Dir, caps, eviction
mount.go          the fs.FS served at Prefix; preview handler
build.go          BuildFinishedHook
internal/css      tokenizer and parser for the property subset; validation
internal/dom      HTML subset parser (golang.org/x/net/html) → element tree
internal/layout   flexbox over the element tree; text measurement via internal/text
internal/text     font loading (golang.org/x/image/font/sfnt), fallback, line
                  breaking, clamping, data-fit
internal/paint    rasterizing boxes, borders, radii, gradients, images and glyphs
                  (golang.org/x/image/vector, golang.org/x/image/draw)
internal/fetch    bounded image loading from Files, ImageOrigins and data: URLs
```

Dependencies: `golang.org/x/image` and `golang.org/x/net/html`, both pure Go.
Nothing else.

### 12.2 Text, in detail

The hardest part and the one most worth being exact about:

- Glyph outlines and advances come from `sfnt`; kerning from the font's `kern`
  table, `GPOS` pair adjustment in v0.2.
- Line breaking is greedy over UAX #14 break opportunities, measured with the
  font's advances at the computed size and letter spacing.
- A run that mixes fonts (fallback for a missing glyph) is split into segments,
  each measured and drawn with its own font.
- `line-clamp` stops after N lines and replaces the end of the last line with `…`,
  removing characters until the ellipsis fits.
- `data-fit` lays the text out at the declared size, and if it overflows its box or
  clamp, again 2px smaller, down to half the declared size.

### 12.3 Phases

| Phase | Delivers | Done when |
| --- | --- | --- |
| 1 | `internal/css`, `internal/dom`, validation with positions | every row of §6 parses, every unsupported input names its line and column |
| 2 | `internal/layout` | layout matches Chrome to the pixel on a fixture set of flex cases |
| 3 | `internal/text` | wrapping, clamping and `data-fit` match Chrome within 1px per line on fixtures, Turkish included |
| 4 | `internal/paint`, `internal/fetch` | golden PNGs for every fixture |
| 5 | plugin: `Set`, store, mount, head tags, default card, export, preview, command | an end-to-end site test: a post page's `og:image` URL serves its card, a restart serves it from `Dir`, an export writes it |
| 6 | documentation, `collage.json`, release v0.1.0 | README complete; listed in collage's plugin CI matrix |

Fixtures for phases 2–4 are pairs of HTML and a PNG screenshot of the same HTML in
headless Chrome, taken once and committed; Chrome is needed to make a fixture, not
to run the tests.

## 13. Later

- **A browser renderer.** A `Renderer` interface — `Draw(ctx, html, w, h)` — with
  the pure-Go one as the default and a chromedp-backed one as a separate module for
  sites that want all of CSS. Same templates, same hashes (the engine version
  names the renderer), same storage.
- **`GPOS` kerning, more scripts** through a shaping library once a pure-Go one is
  mature enough.
- **SVG images and emoji**, drawn from a bundled emoji font.
- **AVIF/WebP output** where networks accept it.

## 14. Decisions this document makes, and why

| Decision | Instead of | Because |
| --- | --- | --- |
| HTML/CSS subset | a Go builder API | a card is designed by looking at it; HTML is what the developer already writes, and the same template can later go to a browser renderer |
| typed `Card` | `any` data | the default card and every page's card share one shape (§4) |
| hash of the executed HTML | hash of template name + data | it is exactly what is drawn; template edits and data changes are both caught, with no serialization of data |
| draw on first GET | draw during the page render | a page is never slower for its card, and an unrequested card is never drawn |
| spec on disk | memory only | a page cached on disk outlives the process that recorded its card |
| default card in `OnAfterRender` | requiring `Set` on every page | the title and description exist only once the page has rendered; plugins cannot hoist after it, so the head is rewritten |
| errors at startup | best-effort drawing | a card drawn wrong is shared before anyone sees it |
