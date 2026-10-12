# Changelog

## v0.4.0

- `Draw(ctx, opts, Input{HTML: …, Locale: …})`: the card's HTML and locale are
  fields of `ogimage.Input` instead of two adjacent string arguments
  (`Draw(ctx, opts, locale, html)`), so passing them swapped no longer compiles.
  An empty `Locale` still draws as `"en"`.
- Fixed: a static build with more cards than the store keeps (`maxEntries` or
  `maxBytes`, with or without `dir`) failed with `ogimage-draw` "file does not
  exist" for cards evicted before they were drawn; the build now draws each card
  from its own copy of the spec.
- Requires collage v0.61.4.

## v0.3.0

The v1 conventions; requires collage v0.61.1 and, in tests, collage-meta v0.3.0.

- One constructor: `New(opts Options)`. `NewWith` and `New()` are gone, and
  `Config` is `Options`.
- `Draw(ctx, opts, locale, html)`: it validates `opts` first (fonts listed with no
  `FontFiles` panicked; they are `ErrInvalidConfig`), keeps to `RenderTimeout`,
  image fetches included (it ran on `context.Background`), and draws in the locale
  given (it was always `en`). The `ogimage` command draws in the application's
  default locale, or `-locale` / `-locale=` (a supported locale), within
  `RenderTimeout`.
- `renderTimeout` is a `collage.Duration`: a string such as `"5s"`; a number is an
  error at startup. `ogimage.Duration` is gone.
- A negative `maxEntries` or `maxBytes` is an error at startup; it meant unlimited.
- `Recorded` remembers the last `MaxEntries` pages: every distinct path that
  recorded a card was kept for the life of the process, so with `default` set,
  each URL a crawler invented grew it.
- `Default*` constants, `SiteInfo` and `PageInfo` are unexported.
- `imageOrigins` is checked with `collage.ParseOrigin`; the development origin's
  scheme follows `collage.IsHTTPS`.
- The build finding code is `ogimage-draw`.

## v0.2.0

- On a site served on several hosts, a card's URL and the `.Site` a template draws
  follow the request's origin (`collage.BaseURL`), so `Config.BaseURL` is no longer
  required when a plugin implementing `collage.OriginResolver` is registered.
  Requires collage v0.42.0. A site with a `Config.BaseURL` is unchanged, and so is
  every card's hash.

## v0.1.2

- In development a card's URL is absolute against the request's own origin, not
  `Config.BaseURL`: a card opened from a page on localhost was looked for on the
  live site, which does not have it. Production URLs, and every card's hash, are
  unchanged.

## v0.1.1

- A card template may hold a `{{/* comment */}}` before its root element, or
  between a flex container's children. It was masked as a value, so startup
  refused the template with "text outside the root element".

## v0.1.0

The first release: card templates in an HTML and CSS subset, drawn in pure Go,
served at content-addressed URLs, a default card for every page, static export,
a development preview and a command to draw one card. See README.md and
DESIGN.md.
