---
name: pages
description: Create, update, and publish self-contained HTML artifacts and small HTML-only sites, and share files such as screenshots, images, PDFs, logs, data exports, archives, and recordings, through the authenticated Pageup CLI. Use when Codex should present project completion or progress in a polished shareable page, publish or revise a status report, handoff, demo, comparison, dashboard, or visual explanation, keep an existing Pageup URL current, upload HTML for human review at pages.gabrielmalek.com, or give a person or agent a link to any file.
---

# Pages

Use Pageup to turn HTML into an unlisted, shareable URL, or to share any other file by link. A single standalone HTML file is the default for pages; a small directory is available when the artifact genuinely benefits from multiple HTML pages. Treat creating, updating, and deleting as authenticated, and viewing as public to anyone who has the URL.

## Build the artifact

1. Gather verifiable facts before designing the page. For project reports, inspect the relevant changes, tests, commit state, and remaining work. Distinguish completed, in-progress, blocked, and proposed work.
2. Prefer one standalone HTML file with inline CSS and optional inline JavaScript. When multiple pages materially improve the artifact, create a directory with `index.html` at its root and no more than 100 `.html` files. Nested folders are supported. Keep CSS and JavaScript inline. Share images, fonts, and other assets with `pageup file` (see below) and reference the returned URLs; site directories themselves accept only HTML.
3. Use a deliberate visual direction suited to the content. Make the result responsive and accessible with semantic HTML, a useful title, viewport metadata, readable contrast, keyboard support, and reduced-motion handling where animation exists.
4. For completion summaries, favor this information order: outcome, what changed, proof or verification, how to use it, and next steps. For progress pages, include an as-of time and clearly labeled status, evidence, blockers, and next actions.
5. Include only information needed for the intended audience. Never include private keys, credentials, tokens, secrets, sensitive logs, or unnecessarily private source material. Assume uploaded pages and files persist.

Save the artifact in the workspace when it is a useful project deliverable; otherwise use a temporary HTML file and remove it after publishing.

## Publish with Pageup

Confirm connectivity and authentication when the environment is unfamiliar:

```sh
pageup doctor
```

Upload a file:

```sh
pageup path/to/report.html
```

Upload a multi-page HTML directory:

```sh
pageup path/to/site
```

The directory must contain `index.html` at its root. Relative links such as `href="about.html"` work, and `docs/index.html` is available at `docs/`. The total uncompressed HTML is capped at 5 MiB.

Upload generated HTML from standard input:

```sh
generate-html | pageup -
```

Use structured output when another command must consume the result:

```sh
pageup --json path/to/report.html
```

Keep an existing URL current when the user names that page or the artifact is an ongoing report:

```sh
pageup update PAGE_URL path/to/report.html
pageup update PAGE_URL path/to/site
generate-html | pageup update PAGE_UUID -
```

`pageup update` accepts either the full Pageup URL or its UUIDv7, replaces the HTML in place, and keeps the same page UUID. It can also convert a standalone page to a site or a site back to a standalone page; site URLs include a trailing slash so relative links resolve correctly. The key that created a page can update it; an admin key can update any page. Pages created before ownership tracking are admin-only until first updated by an admin. Use a new upload when the artifact should have a distinct URL or history boundary.

Do not print, copy, or commit `~/.config/pageup/config.json`. If authentication is missing, install the CLI and create a device key, then have an existing admin authorize its public key; never transfer an existing private key.

## Share files

Use `pageup file` for anything that is not an HTML page: screenshots, images, PDFs, logs, CSV or JSON exports, archives, and recordings. It prints one URL per line.

```sh
pageup file path/to/screenshot.png
pageup file build/report.pdf logs/run.log
some-command 2>&1 | pageup file --name run.log -
pageup file --json path/to/data.csv
```

A single non-HTML path also works without the subcommand: `pageup screenshot.png`. Files without an extension are published as pages only when their content looks like HTML. Directories are not shared as files; archive them first.

Shared files live at `https://pages.gabrielmalek.com/f/<uuid>/<name>`. Images, PDFs, audio, video, plain text, Markdown, CSV, and JSON display in the browser; HTML, SVG, archives, and other types download. Append `?download` to force a download. To put images or other assets in a page, share them as files and reference the returned URLs, for example `<img src="https://pages.gabrielmalek.com/f/.../chart.png" alt="...">`.

Replace a file while keeping its URL, rename it, or delete it:

```sh
pageup update FILE_URL path/to/new-version.pdf
pageup update --name final.pdf FILE_URL path/to/new-version.pdf
pageup delete FILE_URL
```

An update keeps the file name unless `--name` is given; after a rename the old URL redirects to the new one. Only the key that shared a file, or an admin, can update or delete it. The server enforces a per-file size limit and reports it in the error when a file is too large.

Files are public to anyone with the URL. Never share credentials, private keys, `.env` files, or logs that may contain secrets. If something sensitive is shared by mistake, delete it immediately with `pageup delete`.

## Verify and hand off

Open or preview the result and check the important content at desktop and narrow widths. For a site, follow representative relative links and nested directory links as well. Also verify that the returned URL responds successfully and contains the current revision. Correct problems in the local HTML, then update the same UUID when continuity matters or upload a new page when it does not.

Return the final URL prominently with a one-line description. Mention that the page is public-but-unlisted when the audience could mistake the link for access-controlled content.
