# Share card

`card.html` is the social preview: the key visual (`bg.webp`, made with
ChatGPT), the app (`shot.webp`, from `apps/macos/script/shot.sh` with
`SIDEBAR=1`, style dark, without the window shadow), and the text in Inter.

To capture it, open `card.html` in Chrome at the card's size with a device
pixel ratio of 2, take a screenshot, and scale it down:

| File | Size | Used by |
| --- | --- | --- |
| `site/static/og.jpg` | 1200×630 | the site's `og:image`, LinkedIn |
| `.github/assets/social-preview.jpg` | 1280×640 | GitHub's social preview (upload it in the repo settings) |

The layout uses viewport units, so the same file works at both sizes.

`brand-sheet.html` is the image at the top of `BRAND.md`. Capture it the
same way at 1600×848, then save it as `.github/assets/brand/sheet.webp`.
