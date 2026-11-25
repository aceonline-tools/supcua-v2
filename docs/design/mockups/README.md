# supcua desktop design proposal

Base: `370b48f142d9ab5801934637fa0f0fec10dcc094` (v1.0.28). Design only; no application changes. All six PNGs are 1280×800 and were personally viewed after the final render. Fluent-inspired blue accent, quiet surfaces, compact control palette, shared dark/light tokens. Uses Segoe UI when installed, system fallback otherwise; these macOS renders use the system fallback. Synthetic script names, shortcuts, coordinates and MAC address are illustrative.

## View and reproduce

Open PNGs directly. For HTML, open `src/index.html?view=main&theme=dark` (views: main, editor, settings, surfaces; themes: dark, light). Screens are static visual proposals, not interactive app prototypes.

Run from the repository root:

```sh
node docs/design/mockups/src/render.cjs
```

Renderer uses existing local Playwright at `/Users/yanmad27/.agents/skills/playwright-skill/node_modules/playwright` and installed Google Chrome. Override with `PLAYWRIGHT_MODULE` and `CHROME_PATH` on other machines. No installation, server, network asset or external service is required. `render-checks.json` records browser version and zero detected horizontal overflow / below-viewport controls for all six screens. The DOM check supplements visual inspection; it does not validate native Windows behavior.

## Screen and control mapping

| Mockup / element | Actual source or explicit design authority |
|---|---|
| All: app name/version and window chrome | `ui/main_window.go:192`; standard window frame. Letter S is a proposal mark, not replacement artwork for the actual app icon. |
| Main dark/light: Name, Hotkey, Mode columns | `ui/main_window.go:205` |
| Main: group headers, alphabetical order, tree indentation | `ui/main_window.go:352`; expanded only, no folding affordance |
| Main: group checkbox / bulk enable-disable; script checkbox | `ui/main_window.go:118`, `ui/main_window.go:139`; group unchecked when any member is disabled, preserving current binary semantics |
| Main: script names, hotkey badges, Once/Infinite | `ui/main_window.go:96`, `ui/main_window.go:435`; F6 → F7 visually denotes separate start/stop keys (native text uses slash) |
| Main: selected row and double-click to edit | `ui/main_window.go:210`, `ui/main_window.go:255` |
| Main: Global Active switch and active dot | `ui/main_window.go:220`, `ui/main_window.go:64`; switch replaces checkbox visually |
| Main / sidebar: global RCtrl hint | `model/config.go:28`; displayed as a hint, not an editable field |
| Main: Mouse/Keyboard unlocked status | Existing lock action states (`ui/script_edit_dialog.go:265`); explicit brief requires status display. Current main window lacks these readouts; native wiring is not claimed. |
| Main: New, Edit, Delete | `ui/main_window.go:233`, `ui/main_window.go:237`, `ui/main_window.go:241` |
| Main: Clone script | Explicit upcoming feature from brief; acts on selected script beside Edit/Delete |
| Main: script counts, group counts, enabled count, selected name | Read-only summaries of existing list/checkbox/selection data (`ui/main_window.go:94`, `ui/main_window.go:118`, `ui/main_window.go:352`); no filtering or new actions |
| Sidebar: Scripts and Settings navigation | Requested Settings view; Scripts returns to existing main list |
| Editor: Edit Script title | `ui/script_edit_dialog.go:73` |
| Editor: Script Properties; Name, Group, Enabled | `ui/script_edit_dialog.go:93` |
| Editor: Run Mode / Once / Infinite | `ui/script_edit_dialog.go:102` |
| Editor: Hotkey Mode / Toggle / Separate | `ui/script_edit_dialog.go:116` |
| Editor: Toggle Hotkey (click & press keys), shortcut field | `ui/script_edit_dialog.go:138`, `ui/script_edit_dialog.go:506` |
| Editor: Mouse X, Y, Move, capture P hint | `ui/script_edit_dialog.go:171`, `ui/script_edit_dialog.go:420`; default P from `model/config.go:29` |
| Editor: Left Click, Right Click, Left Double, Right Double, Left Down, Right Down, Left Up, Right Up, Store Pos, Move Stored | `ui/script_edit_dialog.go:191` through `ui/script_edit_dialog.go:200` |
| Editor: Keyboard key capture, Press, Key Down, Key Up | `ui/script_edit_dialog.go:206` |
| Editor: Wait duration (100 ms), Wait | `ui/script_edit_dialog.go:226` |
| Editor: Loop count, Start Loop, End Loop | `ui/script_edit_dialog.go:239`; count 0 means infinite |
| Editor: Lock Mouse, Unlock Mouse, Lock Keyboard, Unlock Keyboard | `ui/script_edit_dialog.go:265` |
| Editor: Actions list and sample descriptions; numbered rows are presentation only | `ui/script_edit_dialog.go:282`, `ui/script_edit_dialog.go:891` |
| Editor: Edit, Delete, Up, Down | `ui/script_edit_dialog.go:290` |
| Editor: Close | `ui/script_edit_dialog.go:321`, calls onSave; no invented Save/Cancel workflow |
| Settings dark/light: Theme / Dark / Light / System | Explicit new theme setting from brief |
| Settings: Auto-active when game focused checkbox | Explicit upcoming feature from brief |
| Settings: read-only global/capture/overlay shortcuts, key press duration, scripts directory, overlay visibility | `model/config.go:13` through `model/config.go:21`, defaults `model/config.go:27`; values are explicitly read-only |
| Settings: tray explanatory text | `ui/tray.go:86`, `ui/tray.go:94`; no relocated or duplicate controls |
| Small surfaces: Edit: delay, Delay (ms), OK, Cancel | `ui/action_dialog.go:22`, `ui/action_dialog.go:53`, `ui/action_dialog.go:124`, `ui/action_dialog.go:131` |
| Small surfaces: Not Licensed, read-only MAC, Copy | `ui/license_dialog.go:21`, `ui/license_dialog.go:25`, `ui/license_dialog.go:30` |
| Small surfaces: Update Available, version message, Update, Skip | `ui/update_dialog.go:18`, `ui/update_dialog.go:22`, `ui/update_dialog.go:24`, `ui/update_dialog.go:31` |
| Small surfaces: Toggle Active, Show Window, Toggle Overlay, Overlay: Drag OFF/ON, Exit | `ui/tray.go:67` through `ui/tray.go:116` |
| Small surfaces: RUN: Auto attack; IDLE, DISABLE states | `ui/overlay.go:116` |

## Inventory of alternate states not individually rendered

- Script editor New Script title uses the same layout (`ui/script_edit_dialog.go:73`). Separate mode replaces Toggle Hotkey with Start / Stop Hotkeys (click & press keys), Start and Stop capture fields (`ui/script_edit_dialog.go:151`, visibility switch at line 660). No simultaneous hidden fields are proposed.
- Action parameters vary by existing action type: Mouse Move → X/Y (`ui/action_dialog.go:25`); Key Down/Up → Key (`:40`); Delay → Delay (ms) (`:53`); Loop → Count (0=infinite) (`:65`); Wait For Image → Timeout (ms), Use found position (`:77`); If Image → Use found position (`:93`). All share existing OK/Cancel footer. Image actions have no newly invented creation control.
- Raw JSON editor has a scrolling text area and serialization/parsing functions (`ui/raw_editor.go:21`, `:40`, `:51`) but has no call sites. `ui/script_editor.go` contains no screen implementation. An exposed JSON tab would invent navigation to dormant code, so none is added.
- Hotkey formatting supports none / modifiers / uppercase key (`ui/hotkey_input.go:22`); editor capture is wired in `ui/script_edit_dialog.go:506`. Capture state, conflict override prompt (`:728`), delete confirmation, and error message dialogs keep standard modal patterns; not separately illustrated.
- Overlay can be hidden, visible, draggable, or fixed (`ui/overlay.go:156`, `:176`); tray labels preserve existing actions. No new overlay settings or direct script-running buttons are proposed.
- Window/editor positions and sizes are persisted internally; Settings does not expose extra editing controls for them.

## Open questions and limits

1. Collapsible groups are deliberately excluded: current table has no fold state or handler. Lead revised the brief to expanded groups only. Adding folding would require a separately authorized feature decision.
2. Settings keeps overlay controls in the tray. Duplicating them in Settings remains optional and is not depicted.
3. Auto-active game identification and blur behavior are owned by the upcoming feature; no selector or extra promise is designed here.
4. Main lock readouts require presenting runtime lock state; implementation feasibility and native signal wiring remain for implementation review.
5. Proposed 1280×800 layout is larger than the current 390×300 default and 630×540 editor. Compact/resized layouts, native Fluent feasibility, keyboard/accessibility behavior, and 125–200% Windows DPI are not verified by static PNGs.
6. These images demonstrate a visual direction. Native interactions, save/reopen, theme persistence, and automatic focus behavior are not implemented or tested.

All authored sources, PNGs and evidence are confined to `docs/design/mockups/`. No app source, commits, pushes or external publication were performed.
