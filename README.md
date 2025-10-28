# Waitmesh monorepo

Two folders:

| Folder | Contents |
|--------|----------|
| [`waitmesh/`](waitmesh/) | The Waitmesh engine (Go) — server, CLI, SDK, docs |
| [`website/`](website/) | Marketing site for [waitmesh.com](https://waitmesh.com) |

**Tagline:** Know when something didn't happen.

## Program

```bash
cd waitmesh
go build -o bin/waitmesh ./cmd/waitmesh
./bin/waitmesh serve --db ./waitmesh.db
```

## Website

Next.js marketing site (design system mirrored from PG Circuit).

```bash
cd website
npm install
npm run dev
```
