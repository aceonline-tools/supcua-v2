# FHD simulator (dev tool)

A browser page that shows a simulated 1920x1080 Windows screen with the real supcua UI running in a movable, resizable window. It is labelled **Browser simulation — not WebView2**. It is a dev tool only: nothing here is part of `vite.config.ts`, `src/`, or the production bundle.

```sh
cd frontend && npm run sim      # then open http://127.0.0.1:5700/
```

Requires `git`, `tar`, Node 26 and network access for the first run. Ctrl+C stops everything.

## What it does

- Scaling switch 100/125/150% (taskbar 48/60/72 px, work areas 1920x1032/1020/1008) and version switch v1.2.1, v1.2.3, v1.3.0.
- Each version's window rules are a TypeScript port of the Go code at that tag (`src/sizing.ts`): v1.2.1 fixed 1000x700 physical with an unscaled 640x420 minimum; v1.2.3 DPI-scaled 1000x700 with `fitRect` and `scaledMin`; v1.3.0 DPI-scaled 1000x960.
- Changing version or scaling places the window as a fresh launch would; **Reset to default window** does the same.
- Drag the title bar to move; drag the edges and corners to resize (the version's minimum applies).
- Live readout: version, scaling, outer physical size, client physical size, CSS viewport, DPR.
- `?v=v1.2.3&s=125` selects a version and scaling from the URL.

## How versions are served

On first run, for each tag: `git archive <tag> frontend` into `sim/.cache/<tag>/`, `npm ci` with that tag's own lockfile, then the cached copy's `src/api/mock.ts` seed is rewritten (18 scripts in 4 groups, system theme, the tag as the version). Each copy runs its own Vite dev server on ports 5701-5703 (the page is on 5700; set `SIM_PORT` to move all four). The dev server keeps the MockBackend, which a production build strips. The cache is git-ignored; delete `sim/.cache` to rebuild. Tags are read from the local clone (fetched from `origin` if missing). Mock state resets when you switch versions.

## Tests

`npm run sim:test` runs the sizing tests, built from the `dpisize_test.go` vectors at v1.2.3 and v1.3.0 plus the 984x661 data point. They are separate from `npm test` (whose `include` is `src/**`) and from `npm run check` (tsconfig `include` is `src/**`), so neither is affected.

## Limits

- Chromium/WebKit/Gecko, not WebView2: fonts, scrollbars (overlay on macOS vs 17 px on Windows) and text rendering differ.
- The iframe lays out at the CSS viewport and is drawn at physical size with `transform: scale(dpi/96)`. The browser cannot set the iframe's `devicePixelRatio`, so it reports the browser's value; CSS px, width media queries and visual size match the real app, `resolution`/DPR-based queries do not.
- Frame metrics are assumed Windows 11 values: border `round(8*dpi/96)`, caption `round(31*dpi/96)`; 1000x700 at 96 DPI gives the CI-logged 984x661 client. The first window is placed at the 60,60 cascade origin used by the Go tests.
- Title-bar buttons are visual only. The window is not clamped to a maximum size.
