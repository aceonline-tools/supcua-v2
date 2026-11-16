# WebView2 spike — Windows smoke checklist (~10 min)

Exes (not committed): `spike/out/plain/spike.exe`, `spike/out/garble/spike.exe` (build: `spike/build.sh` on a host with Go 1.25.0, garble v0.15.0, rsrc v0.10.2). Run the whole list for **plain**, then repeat steps 1–9 for **garble**; results must match. Automated equivalent: `spike/ci/webview2-spike.yml` (`-selftest`, `-selftest-noruntime`).

Needs: Windows 10/11 with the WebView2 Evergreen runtime (Win11 has it).

| # | Step | Expected |
|---|------|----------|
| 1 | Run `spike.exe` | Window "supcua WebView2 spike" with the page; a small dark overlay "TICK n" at top-left (10,10) incrementing each second; tray icon appears. |
| 2 | Click **Ping** (text `hello`) | `pingOut` shows `{"echo":"hello","upper":"HELLO","seq":1}`; again → `seq":2`. (JS→Go return value) |
| 3 | Watch "Go→JS event" | JSON `{"active":true,"running":"","tick":N}` changes every second; counter rises. |
| 4 | Overlay: **Toggle drag** then drag the overlay with the mouse | Overlay follows the mouse; toggle again → clicks pass through it (click-through). **Toggle visible** hides/shows it. Text keeps repainting (no flicker, no black box). |
| 5 | Click window **minimize** (—) | Window hides, process keeps running, tray icon stays. Tray left-click restores; tray menu **Hide** / **Show** work. (**X** exits the app, like today.) |
| 6 | Page **Hide to tray**, then tray → Show | Same as 5. |
| 7 | Keyboard check input: type text, press Tab, Enter, Esc, arrows | Text is typed; `keydown <key>` shown for each key including Tab/Enter/Esc (no beep, no focus stolen by the host). **Report any key that does not arrive** — this validates the "WebView2 in a raw top-level window, not a walk child" decision. |
| 8 | **Open dialog** → Close; switch Windows theme light/dark | Modal appears/disappears; page colours follow the OS theme live. |
| 9 | Tray → **Exit** (and, in a second run, page **Exit**) | Overlay and tray icon vanish, process exits in <3 s (check Task Manager: no `spike.exe`; `msedgewebview2.exe` children end shortly after). |
| 10 | Missing runtime: `set WEBVIEW2_BROWSER_EXECUTABLE_FOLDER=C:\empty` (an empty dir) then `spike.exe` | Detection reports missing → native MessageBox "WebView2 Runtime missing" with the `go.microsoft.com/fwlink/p/?LinkId=2124703` link; **Yes** opens the browser. Exit code 3. No window, no silent exit. |
| 11 | Headless: `Start-Process .\spike.exe -ArgumentList '-selftest' -Wait -PassThru` | Exit 0; `spike.selftest.json` next to the exe has `"pass":true`; GDI/USER/handles flat after 50 dialog cycles; `own_callbacks_before_after` = `[2,2]`. |
| 12 | Same with `-selftest-noruntime` | Exit 0, `"pass":true`. |
| 13 | **garble parity** | Steps 1–12 behave identically with `out/garble/spike.exe` (JS→Go call works, events arrive, JSON keys unchanged). |

Known unknowns this list should settle: key delivery (7), Win10 without runtime (10), multi-monitor/DPI drag of the overlay (4).
