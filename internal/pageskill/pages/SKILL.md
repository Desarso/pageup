---
name: pages
description: Create, update, and publish self-contained HTML artifacts through the authenticated Pageup CLI. Use when Codex should present project completion or progress in a polished shareable page, publish or revise a status report, handoff, demo, comparison, dashboard, or visual explanation, keep an existing Pageup URL current, or upload an HTML file for human review at pages.gabrielmalek.com.
---

# Pages

Use Pageup to turn HTML into an unlisted, shareable URL. Treat creating and updating pages as authenticated, and viewing as public to anyone who has the URL.

## Build the artifact

1. Gather verifiable facts before designing the page. For project reports, inspect the relevant changes, tests, commit state, and remaining work. Distinguish completed, in-progress, blocked, and proposed work.
2. Create one standalone HTML file with inline CSS and optional inline JavaScript. Prefer no runtime dependencies so the page remains portable.
3. Use a deliberate visual direction suited to the content. Make the result responsive and accessible with semantic HTML, a useful title, viewport metadata, readable contrast, keyboard support, and reduced-motion handling where animation exists.
4. For completion summaries, favor this information order: outcome, what changed, proof or verification, how to use it, and next steps. For progress pages, include an as-of time and clearly labeled status, evidence, blockers, and next actions.
5. Include only information needed for the intended audience. Never include private keys, credentials, tokens, secrets, sensitive logs, or unnecessarily private source material. Assume uploaded pages persist.

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
generate-html | pageup update PAGE_UUID -
```

`pageup update` accepts either the full Pageup URL or its UUIDv7, replaces the HTML in place, and prints the unchanged URL. The key that created a page can update it; an admin key can update any page. Pages created before ownership tracking are admin-only until first updated by an admin. Use a new upload when the artifact should have a distinct URL or history boundary.

Do not print, copy, or commit `~/.config/pageup/config.json`. If authentication is missing, install the CLI and create a device key, then have an existing admin authorize its public key; never transfer an existing private key.

## Verify and hand off

Open or preview the result and check the important content at desktop and narrow widths. Also verify that the returned URL responds successfully and contains the current revision. Correct problems in the local HTML, then update the same URL when continuity matters or upload a new page when it does not.

Return the final URL prominently with a one-line description. Mention that the page is public-but-unlisted when the audience could mistake the link for access-controlled content.
