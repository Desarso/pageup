# Sharing an HTML page

Use the `$pages` skill to create polished project summaries, progress reports, demos, and other shareable HTML artifacts. The underlying CLI is named **Pageup** and its executable command is `pageup`.

When a human needs to inspect or share a generated HTML artifact, upload it with:

```sh
pageup path/to/page.html
```

The command prints only the shareable URL on success. Use `pageup --json page.html` when a machine-readable response is preferable, or pipe content with `pageup -`.

To replace an existing page without changing its UUID or URL, use:

```sh
pageup update <URL-or-UUID> path/to/page.html
```

The page's creator key or an admin key may update it. `pageup update <URL-or-UUID> -` accepts HTML from standard input.

Run `pageup doctor` if credentials or connectivity are in doubt. Never print, commit, or copy the private key from `~/.config/pageup/config.json`.
