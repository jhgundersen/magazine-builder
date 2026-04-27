# magazine-builder

Generic magazine/newspaper planning web app inspired by `spillhistorie-magasin`.

It lets a user:

- describe a magazine/newspaper style or provide a reference image URL
- enhance that style with `defapi text`
- add articles manually with title/body/image URLs
- import latest articles from an RSS/Atom feed
- choose a page count from a fixed set
- generate a compact JSON style guide with separate guidance for cover, content, feature, short article, advert, filler, back page, and template pages
- generate a style-aware JSON creative kit and per-page prompts for cover, articles, adverts, filler pages, and back page
- drag/drop middle pages to reorder them while keeping cover and back page fixed
- render page images with live previews, using the cover plus a shared blank content template as style references
- write the rendered pages into a PDF

## Run

Install `defapi` first:

```sh
go install github.com/jhgundersen/defapi-cli@latest
```

```sh
go run .
```

Open http://localhost:8080.

Useful flags:

```sh
go run . -addr :8090
go run . -workdir magazine-work
go run . -defapi-text defapi -defapi-text-category text -defapi-text-model claude
go run . -defapi-image defapi -defapi-image-category image -defapi-image-model gpt2
go run . -defapi-image-max-prompt-chars 4000
```

The browser orchestrates rendering: cover first, then a non-PDF content template image, then the remaining pages in small parallel batches. Content pages receive the template image as their first style reference.
