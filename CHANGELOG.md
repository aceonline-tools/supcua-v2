# Changelog

## [1.5.2](https://github.com/aceonline-tools/supcua-v2/compare/v1.5.1...v1.5.2) (2026-11-27)


### Bug Fixes

* log overlay status changes and drawing errors ([2d24d8d](https://github.com/aceonline-tools/supcua-v2/commit/2d24d8d578a1a65a7a2557cbf9883acd8adfefaa))

## [1.5.1](https://github.com/aceonline-tools/supcua-v2/compare/v1.5.0...v1.5.1) (2026-11-27)


### Bug Fixes

* bold only for titles, aligned icon columns and no layout jumps ([dc40da1](https://github.com/aceonline-tools/supcua-v2/commit/dc40da1f92f06afca827ff1d216e5c10dfca73c1))
* load the UI about 2s faster by not using an mDNS host name ([d0a7413](https://github.com/aceonline-tools/supcua-v2/commit/d0a741352f8ff94dce7497c80c70a86568a354df))

## [1.5.0](https://github.com/aceonline-tools/supcua-v2/compare/v1.4.1...v1.5.0) (2026-11-27)


### Features

* overlay style, button icons, key sequences and startup loader ([a22dfa5](https://github.com/aceonline-tools/supcua-v2/commit/a22dfa5590e0832f360ee8d815d526cabf08c0f0))

## [1.4.1](https://github.com/aceonline-tools/supcua-v2/compare/v1.4.0...v1.4.1) (2026-11-27)


### Bug Fixes

* show the licence screen on unlicensed machines ([1d531dc](https://github.com/aceonline-tools/supcua-v2/commit/1d531dc58ceb14154697d5fc6dc0e679ec105271))

## [1.4.0](https://github.com/aceonline-tools/supcua-v2/compare/v1.3.0...v1.4.0) (2026-11-26)


### Features

* editor drag-and-drop reorder, insertion caret and group combobox ([f450c67](https://github.com/aceonline-tools/supcua-v2/commit/f450c678db73cc563ffc9971278a2dc335aeedcb))


### Bug Fixes

* remove the tree connector before script names in the Scripts list ([3373fac](https://github.com/aceonline-tools/supcua-v2/commit/3373fac45a19ebb77319ae907949faa81fe2fe4c))

## [1.3.0](https://github.com/aceonline-tools/supcua-v2/compare/v1.2.3...v1.3.0) (2026-11-26)


### Features

* collapsible sidebar and a taller default window so all scripts fit without scrolling ([#15](https://github.com/aceonline-tools/supcua-v2/issues/15)) ([5abc707](https://github.com/aceonline-tools/supcua-v2/commit/5abc707867474da03e3321a956869551a69dc85d))

## [1.2.3](https://github.com/aceonline-tools/supcua-v2/compare/v1.2.2...v1.2.3) (2026-11-26)


### Bug Fixes

* scale the default window and minimum size by the monitor DPI ([c37f3de](https://github.com/aceonline-tools/supcua-v2/commit/c37f3de2c6905ce770df91bb97b3b9b96fea83ee))

## [1.2.2](https://github.com/aceonline-tools/supcua-v2/compare/v1.2.1...v1.2.2) (2026-11-25)


### Bug Fixes

* re-assert WebView2 controller visibility and bounds when the host window is shown ([#11](https://github.com/aceonline-tools/supcua-v2/issues/11)) ([ec20cbb](https://github.com/aceonline-tools/supcua-v2/commit/ec20cbb5a4654dff236e1b2aa7adc12c31c24d15))

## [1.2.1](https://github.com/aceonline-tools/supcua-v2/compare/v1.2.0...v1.2.1) (2026-11-25)


### Bug Fixes

* never leave a silent blank window at startup; production-path CI gate ([#9](https://github.com/aceonline-tools/supcua-v2/issues/9)) ([27c0432](https://github.com/aceonline-tools/supcua-v2/commit/27c043289ba376bcab13891d45882eea77a5ea1b))

## [1.2.0](https://github.com/aceonline-tools/supcua-v2/compare/v1.1.0...v1.2.0) (2026-11-25)


### Features

* **ui:** replace Win32 UI with WebView2 + Svelte frontend ([#7](https://github.com/aceonline-tools/supcua-v2/issues/7)) ([84a8844](https://github.com/aceonline-tools/supcua-v2/commit/84a88449a9b3cd55b3071c613b0818a4215f2c1f))

## [1.1.0](https://github.com/aceonline-tools/supcua-v2/compare/v1.0.28...v1.1.0) (2026-11-16)


### Features

* clone script and auto-active toggle for game focus ([2aad148](https://github.com/aceonline-tools/supcua-v2/commit/2aad1484ddc2ee76aa994e43ce89a15dd255eb48))
* **updater:** check GitHub Releases instead of MinIO ([d6e008f](https://github.com/aceonline-tools/supcua-v2/commit/d6e008ff51e19c9e73cd6109f911493a0bd3f4dc))


### Bug Fixes

* memory and resource leaks found in v1.0.28 audit ([563270d](https://github.com/aceonline-tools/supcua-v2/commit/563270d00cf23514a4a073ee7504113586b65d83))
