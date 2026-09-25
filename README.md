# Pageup

Pageup turns a local HTML file or small HTML-only directory into an unlisted, shareable URL with one command, and shares any other file the same way:

```console
$ pageup report.html
https://pages.gabrielmalek.com/019c...

$ pageup ./experiment
https://pages.gabrielmalek.com/019d.../

$ pageup file screenshot.png build.log
https://pages.gabrielmalek.com/f/019e.../screenshot.png
https://pages.gabrielmalek.com/f/019e.../build.log
```

Viewing pages is public so links can be shared. Creating, updating, and managing access require Ed25519-signed requests; the server stores public keys only. Every new upload gets a UUIDv7 URL, existing pages can be updated in place by their creator or an admin, and there is no public page index.

## Install

macOS or Linux:

```sh
curl -fsSL https://pages.gabrielmalek.com/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://pages.gabrielmalek.com/install.ps1 | iex
```

Every Pageup CLI binary contains the complete `$pages` agent skill. Install it into the detected Codex or `~/.agents` skill directory with:

```sh
pageup skill install
```

Use `pageup skill show` to inspect the embedded instructions, `--harness project` to install under `./.agents/skills`, or `--target DIR` for another agent harness. Existing skill files are preserved unless `--force` is supplied.

The initial machine is configured during deployment. For a new computer, create a separate, revocable key:

```sh
pageup init --endpoint https://pages.gabrielmalek.com --name "Gabriel laptop"
```

The command prints a public key and an approval command. Run that approval command on a computer which already has an admin credential, then verify the new computer:

```sh
pageup doctor
pageup example.html
```

This pairing flow never moves a private key between computers. Use `pageup keys list` and `pageup keys revoke KEY_ID` to audit or revoke devices. New keys default to the upload-only role; pass `--role admin` only when the device should also manage keys.

## CLI

```text
pageup file.html                     upload a file; print its URL
pageup ./site                        upload an HTML-only directory
pageup -                             upload HTML from stdin
pageup --json file.html              return id, URL, and revision state as JSON
pageup --open file.html              upload and open in the default browser
pageup update URL file.html          update HTML without changing the URL
pageup update URL ./site             update or convert to a multi-page site
pageup update UUID -                 update by id with HTML from stdin
pageup photo.png                     share a non-HTML file; print its URL
pageup file a.pdf b.log              share several files, one URL per line
pageup file --name out.log -         share stdin under a file name
pageup update FILE_URL new.pdf       replace a file without changing its URL
pageup update --name v2.pdf URL f    replace and rename a file
pageup delete FILE_URL               delete a shared file
pageup doctor                        test connectivity and authentication
pageup whoami                        show the active key
pageup public-key                    print this device's public key
pageup skill show                    print the embedded Pages skill
pageup skill install                 install Pages into an agent harness
pageup keys add --name NAME PUBKEY   authorize another device
pageup keys list                     list authorized devices
pageup keys revoke KEY_ID            revoke a device
```

For a multi-page site, pass a directory containing `index.html` at its root. Pageup recursively preserves up to 100 `.html` files, so links such as `href="about.html"` and `href="docs/"` work as expected. A nested `docs/index.html` is served at the directory-style URL `/docs/`. Directories may contain only HTML: keep CSS and JavaScript inline and use remote URLs for images, fonts, and other assets. The combined uncompressed HTML remains subject to the 5 MiB limit.

## File sharing

Any file that is not HTML can be shared: screenshots, images, PDFs, logs, data exports, archives, and recordings. A single non-HTML path is shared automatically, while `pageup file` accepts several paths or `-` for standard input with `--name`. Files without an extension are published as pages only when their content looks like HTML.

Shared files live at `/f/<uuid>/<name>`. The UUID identifies the file; a missing or outdated name redirects to the current one. Images, PDFs, audio, video, plain text, Markdown, CSV, and JSON are served inline, and every other type, including HTML and SVG, is served as an attachment. `?download` forces an attachment. Responses support Range requests, so media can seek, and they use `Cache-Control: no-cache` with a SHA-256 ETag, so updates appear immediately while unchanged files revalidate cheaply. File responses allow cross-origin reads because they are already public to anyone with the URL.

Updates keep the file name unless `--name` is given. The creator key or an admin can update or delete a file. Uploads stream in both directions: the CLI signs the file's SHA-256 in a header, the server authenticates the request before reading the body, verifies the hash while spooling to a temporary file, and then streams it to storage. Neither side holds the file in memory. Each file is capped by `PAGEUP_MAX_FILE_BYTES` (100 MiB by default). The CLI reports the limit in its 413 error.

Credentials live at `~/.config/pageup/config.json` on Linux, the normal application config directory on macOS/Windows, and always use mode `0600` where supported. `PAGEUP_CONFIG` selects another config file. Headless agents can use `PAGEUP_PRIVATE_KEY` with `PAGEUP_ENDPOINT` instead; treat the private-key value as a secret.

## Security model

Each request signs the HTTP method, path, Unix timestamp, random UUIDv7 nonce, and SHA-256 body hash. The server rejects unknown keys, modified requests, timestamps outside five minutes, and replayed nonces. Admin-only endpoints add, list, or revoke keys. Upload-only keys cannot manage access.

Pages and sites are capped at 5 MiB of HTML. Sites are additionally capped at 100 HTML files; archives are validated against path traversal, duplicate paths, non-HTML entries, and expanded-size abuse before storage. Content is stored outside the container on a persistent volume and served as uncached `text/html` so in-place updates appear immediately. Each page records its creator key: that key and admin keys may update it at the same UUID. Legacy pages without ownership metadata are admin-only until an admin updates them once. Anyone with a page URL can view it; UUID randomness and the absence of a listing provide link privacy, not access control.

## Development

The project uses only the Go standard library.

```sh
make check
make build
make docker
```

The container builds CLI downloads for Linux, macOS, and Windows on amd64 and arm64. `scripts/render-coolify-dockerfile.sh` produces the self-contained Dockerfile used by the no-Git Coolify deployment.

Server settings:

| Variable | Default | Purpose |
| --- | --- | --- |
| `PAGEUP_PUBLIC_URL` | derived from request | Canonical origin returned after upload |
| `PAGEUP_DATA_DIR` | `/data` | Persistent pages, files, and authorized-key store |
| `PAGEUP_DOWNLOADS_DIR` | `/app/downloads` | Cross-platform CLI binaries |
| `PAGEUP_BOOTSTRAP_KEYS` | required on first boot | JSON array containing at least one admin public key |
| `PAGEUP_MAX_PAGE_BYTES` | `5242880` | Maximum HTML bytes per page or site |
| `PAGEUP_MAX_FILE_BYTES` | `104857600` | Maximum bytes per shared file |
| `PAGEUP_LISTEN_ADDR` | `:8080` | HTTP listen address |
