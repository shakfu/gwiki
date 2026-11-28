# ACP client

gwiki is the editor in the [Agent Client Protocol](https://agentclientprotocol.com) (protocol version 1). The agent runs as a subprocess, speaking JSON-RPC over stdio.

## What gwiki offers the agent

- **Capabilities:** `fs.readTextFile` and `fs.writeTextFile`. Terminals, elicitation, session loading and modes are not offered.
- **MCP:** `session/new` passes `gwiki -C <wiki root> mcp`, the running executable, so the agent has the wiki tools with their hash-checked writes.
- **Prompts:** text, plus a `resource_link` to the open page's file.

## What gwiki enforces

| | scope |
|---|---|
| `fs/read_text_file` | the repository, read through an `os.Root`; the open page's unsaved text when it is dirty |
| `fs/write_text_file` | `.gwiki/wiki` and `.gwiki/templates`. A page goes through `WritePageFile` and the commit's hash check; a template must be `<name>.md` or `.qmd`; any other file goes through an `os.Root`. The open page is not written over unsaved edits. |
| `session/request_permission` | an `edit`, `delete` or `move` whose locations or diff paths are outside the write roots is rejected unasked, with a notice in the column |

The rest of `.gwiki` (`cache.db`, `config.json`, `drafts/`, `render/`) is not writable: the agent could only break gwiki's state there.

## What it does not enforce

An agent's own tools, such as Claude Code's file edits or shell, act on the disk without passing through gwiki. gwiki sees them only as permission requests, and only when the agent asks. They are governed by the agent's own permission settings; gwiki is not a sandbox.

## Agents per user

Agents are named only in the per-user `config.json`. A project's `.gwiki/config.json` naming one is an error: it is committed, so `:agent` in a cloned repository would run whatever program it named.

## SDK

`github.com/coder/acp-go-sdk`, pinned. There is no official Go SDK; the official ones are Kotlin, Python, Rust and TypeScript. The SDK is before 1.0, so its types stay inside `internal/acp`, which converts them to its own events. `internal/acp/acptest` is a stand-in agent built on the SDK's agent side; the `acp` and `tui` tests run it as their own executable.

The SDK logs to `slog`'s default logger; the session sends that to the agent's error output instead, which would otherwise draw over the interface.
