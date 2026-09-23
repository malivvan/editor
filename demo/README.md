# editor demo

A single runnable example that hosts `github.com/malivvan/editor` on a
`tcell.Screen` built by hand. There is no application framework involved: the
demo opens the screen, paints its own frame and status bar with plain tcell
calls, and forwards every event to one `editor.View`.

```sh
go run ./demo              # edit the built-in sample buffer
go run ./demo main.go      # edit a real file
```

| Keys    | Action                                                   |
| ------- | -------------------------------------------------------- |
| Ctrl-S  | Save the buffer (no-op with a message for the sample).   |
| Ctrl-Q  | Quit.                                                    |
| F1      | Toggle line numbers.                                     |
| F2      | Cycle through the embedded colorschemes.                 |

Typing a word opens the autocomplete popup; clicking moves the cursor, and
dragging selects text.

The interesting part for host authors is how little is needed:

```go
view := editor.NewView(buf)
view.SetRuntimeFiles(runtime.Files)      // syntax highlighting + colorschemes
view.SetRect(1, 1, width-2, height-3)    // the pane the host reserved
view.Draw(screen)                        // screen is a *tcell.Screen
view.HandleEvent(event)                  // keys and mouse, one entry point
```
