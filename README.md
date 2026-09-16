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
- render page images with live previews, using a shared brand asset board as a style reference
- write the rendered pages into a PDF

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/jhgundersen/magazine-builder/master/install.sh | sh
```

Installs `magazine-builder` and `defapi-cli`. Make sure `~/.local/bin` is in your `PATH`.

## Run

```sh
magazine-builder
```

Open http://localhost:8080.

## Update

```sh
magazine-builder update
```

Downloads the latest release for your OS/architecture and replaces the installed `magazine-builder` binary. If you are not running an installed `magazine-builder` binary, it updates `${PREFIX:-$HOME/.local}/bin/magazine-builder`.

Useful flags:

```sh
go run . -addr :8090
go run . -workdir magazine-work
go run . -defapi-text defapi -defapi-text-category text -defapi-text-model claude
go run . -defapi-image defapi -defapi-image-category image -defapi-image-model gpt2
go run . -defapi-image-max-prompt-chars 4000
```

The browser orchestrates page rendering. Each page receives its own structured style, palette, issue identity and content instructions. A shared brand asset board carries the masthead, wordmark, issue number mark and divider; posters skip brand assets and page furniture.

## Development and hosting

Run `make check` and `go vet ./...` before submitting changes. Project conventions and the source map are in [AGENTS.md](AGENTS.md).

Production runs at **https://mag.jonh.no** on the existing jonh.no Docker host. [The deployment workflow](.github/workflows/deploy.yml) tests pull requests and deploys tested binaries from `master`. See [deployment setup and rollback](deploy/README.md) for server configuration and GitHub secrets.
