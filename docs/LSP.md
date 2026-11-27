# D2 language server

`d2 lsp` runs a Language Server Protocol server on stdin and stdout. It needs no configuration.

The server provides diagnostics as you type, formatting, completion, hover, document outline, folding, color pickers, and links for imports and URLs. Go to definition, find references, and shape rename follow imports, including unsaved changes in open files.

## Visual Studio Code

Install [d2lang, the official D2 extension](https://marketplace.visualstudio.com/items?itemName=d2lang.d2lang), from the Marketplace or with this command.

```sh
code --install-extension d2lang.d2lang
```

Open a `.d2` file to start the server. The extension uses the `d2` executable on your `PATH`, which must support `d2 lsp`. To use a different build, set `D2.execPath` to its executable path in VS Code.

## Neovim (0.11 or later)

```lua
vim.filetype.add({ extension = { d2 = "d2" } })
vim.lsp.config("d2", {
  cmd = { "d2", "lsp" },
  filetypes = { "d2" },
  root_markers = { ".git" },
  flags = { debounce_text_changes = 0 },
})
vim.lsp.enable("d2")
```

Sending changes immediately keeps cross-file renames in sync when you switch between unsaved buffers. The server already debounces diagnostics.

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
- References and rename cover open documents and the files they import. Open the diagrams that use a shared file before renaming one of its shapes.
- A file that is only ever imported by another file is checked on its own, so it may show errors that do not occur when it is imported.
- Renaming a shape that has no label also changes the text shown on it, because the name is the label.
- Files larger than 2 MiB receive an informational diagnostic. Language features resume when the file is within that limit.
- Shape rename updates literal `near` paths. If a `near` value comes from a variable or a shared value resolves to different targets on different boards, the server reports the conflict before applying edits.
- Rename can also report a conflict when a related file has a syntax error, a selective import uses the name, or a board removes or suspends a field with that name. The diagnostic or rename message identifies what needs attention.
