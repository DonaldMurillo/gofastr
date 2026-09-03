# GoFastr static site example

A multi-page static website served from a folder of HTML files. Zero JavaScript framework: just HTML, CSS, and GoFastr's static file server.

## Architecture

- **Pages**: Regular HTML files in the `pages/` directory. Edit them like any static site
- **Serving**: GoFastr's `core/static` module serves files with ETag caching and MIME detection
- **No build step**: Drop HTML/CSS files in `pages/`, they're served as-is

## File structure

```
examples/static-site/
├── main.go              # Go server, mounts the pages/ folder
├── pages/
│   ├── index.html       # Landing page (/)
│   ├── about.html       # About page (/about.html)
│   ├── contact.html     # Contact page (/contact.html)
│   └── style.css        # Shared stylesheet
└── README.md
```

## Running

```bash
cd examples/static-site
gofastr dev          # or: go run .
# Open http://localhost:3070 (or the address the banner prints)
```

## Adding pages

Just create a new `.html` file in `pages/` and link to it:

```bash
echo '<!DOCTYPE html><html>...' > pages/pricing.html
# → available at http://localhost:3070/pricing.html
```
