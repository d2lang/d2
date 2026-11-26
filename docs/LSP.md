# D2 language server

`d2 lsp` runs a Language Server Protocol server on stdin and stdout. It needs no configuration.

The server provides diagnostics as you type, formatting, completion, hover, document outline, folding, color pickers, and links for imports and URLs. Go to definition, find references, and shape rename follow imports, including unsaved changes in open files.

## Neovim (0.11 or later)

```lua
vim.filetype.add({ extension = { d2 = "d2" } })
vim.lsp.config("d2", {
  cmd = { "d2", "lsp" },
  filetypes = { "d2" },
  root_markers = { ".git" },
})
vim.lsp.enable("d2")
```

## Helix

In `languages.toml`:

```toml
[language-server.d2]
command = "d2"
args = ["lsp"]

[[language]]
name = "d2"
scope = "source.d2"
file-types = ["d2"]
comment-token = "#"
language-servers = ["d2"]
```

## Any other editor

Point the editor's LSP client at the command `d2` with the argument `lsp`, for files ending in `.d2`. The server speaks LSP 3.17 over stdio with UTF-16 positions.

## Claude Code

In a plugin's `.lsp.json`:

```json
{
  "d2": {
    "command": "d2",
    "args": ["lsp"],
    "extensionToLanguage": { ".d2": "d2" }
  }
}
```

## Limits

- Go to definition and find references work on shapes and connections. Rename is available for shapes. Class names and variable substitutions support go to definition.
- A file that is only ever imported by another file is checked on its own, so it may show errors that do not occur when it is imported.
- Renaming a shape that has no label also changes the text shown on it, because the name is the label.
- Files larger than 2 MiB receive an informational diagnostic. Language features resume when the file is within that limit.
