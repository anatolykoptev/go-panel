# Changelog

## [0.23.9](https://github.com/anatolykoptev/go-panel/compare/v0.23.8...v0.23.9) (2026-08-22)


### Fixed

* **shell:** a detail table no longer draws a second card inside its section ([123b08a](https://github.com/anatolykoptev/go-panel/commit/123b08afbc1128fef84e0d174651415ec3e0dee9))

## [0.23.8](https://github.com/anatolykoptev/go-panel/compare/v0.23.7...v0.23.8) (2026-08-22)


### Added

* **resource:** BasePathFrom — hand out what CrossLinkCell/FilterLinkCell require ([#141](https://github.com/anatolykoptev/go-panel/issues/141)) ([caa5233](https://github.com/anatolykoptev/go-panel/commit/caa523372f399b2298bbe7f77d424c5ab28c76de))
* **resource:** the first cell links to the detail page, without the consumer building the URL ([dd4b8fe](https://github.com/anatolykoptev/go-panel/commit/dd4b8fe10b4dd21a4129a64b2fa2e96b1173af67))

## [0.23.7](https://github.com/anatolykoptev/go-panel/compare/v0.23.6...v0.23.7) (2026-08-21)


### Added

* **resource:** DetailSection.Table + FilterLinkCell ([#139](https://github.com/anatolykoptev/go-panel/issues/139)) ([05bde9f](https://github.com/anatolykoptev/go-panel/commit/05bde9f75f9c985ef0d8da7cd4840a8ea14ff874))

## [0.23.6](https://github.com/anatolykoptev/go-panel/compare/v0.23.5...v0.23.6) (2026-08-21)


### Added

* **components:** NoticeView — the message block three consumers were hand-writing ([593f84b](https://github.com/anatolykoptev/go-panel/commit/593f84b6649bfd6e56c7a29fd1cdf7d9c8091d7b))
* **resource:** Resource.Views — named modes a filter cannot express; mark the selected chip ([7893f40](https://github.com/anatolykoptev/go-panel/commit/7893f408c8bde72f9cbde0cd0b66e69223841d6e))

## [0.23.5](https://github.com/anatolykoptev/go-panel/compare/v0.23.4...v0.23.5) (2026-08-20)


### Added

* **resource,shell:** a panel-wide Trash for rows a delete kept ([6a9d95e](https://github.com/anatolykoptev/go-panel/commit/6a9d95e41c50135715b06c1ff53ccd8b5ede1f8c))
* **resource,shell:** a panel-wide Trash for rows a delete kept ([5d4d5bb](https://github.com/anatolykoptev/go-panel/commit/5d4d5bb090fd60ec83b87ebad968754ab632d93b))


### Fixed

* **resource:** hide a sidebar group heading when all its links are filtered out ([97f7d37](https://github.com/anatolykoptev/go-panel/commit/97f7d37429c22f608c3f7b6d336387a428172d88))
* **resource:** pin the Trash's enforcement, not just its behaviour ([7608304](https://github.com/anatolykoptev/go-panel/commit/76083045cc906e97e39338c22f3844df14596e95))
* **resource:** recognise a group header by shape, not by ID ([52e9d3e](https://github.com/anatolykoptev/go-panel/commit/52e9d3e738e9010c08d2319c19b2c750f50f372e))

## [0.23.4](https://github.com/anatolykoptev/go-panel/compare/v0.23.3...v0.23.4) (2026-08-20)


### Added

* **resource,shell:** delete with an undo window instead of a confirm dialog ([#131](https://github.com/anatolykoptev/go-panel/issues/131)) ([8963a95](https://github.com/anatolykoptev/go-panel/commit/8963a95feacbce84f4fdb1491f4fe14e2e55e4be))


### Fixed

* **resource:** the delete form posted a CSRF field nothing reads, so delete always 403'd ([#129](https://github.com/anatolykoptev/go-panel/issues/129)) ([07206c4](https://github.com/anatolykoptev/go-panel/commit/07206c41f463506369d4ac51ab8f1ee17acfcd59))

## [0.23.3](https://github.com/anatolykoptev/go-panel/compare/v0.23.2...v0.23.3) (2026-08-20)


### Fixed

* **resource,shell:** the delete confirmation never ran — CSP drops inline handlers ([#127](https://github.com/anatolykoptev/go-panel/issues/127)) ([05ca78d](https://github.com/anatolykoptev/go-panel/commit/05ca78daa6be53c2bde9379949b3884d71742097))

## [0.23.2](https://github.com/anatolykoptev/go-panel/compare/v0.23.1...v0.23.2) (2026-08-20)


### Fixed

* **resource,shell:** keep a wide table's overflow inside the table ([#125](https://github.com/anatolykoptev/go-panel/issues/125)) ([d155278](https://github.com/anatolykoptev/go-panel/commit/d1552784e32e2a5a15954a75730b1e975f5d34fc))

## [0.23.1](https://github.com/anatolykoptev/go-panel/compare/v0.22.2...v0.23.1) (2026-08-20)


### Fixed

* **mcp,release:** the version comes from the release, never from a literal ([#124](https://github.com/anatolykoptev/go-panel/issues/124)) ([423c68f](https://github.com/anatolykoptev/go-panel/commit/423c68fe9d5c6497239c6aea0252baee667d3b48))
* **release:** put the version above the orphan v0.23.0 tag, action to v5 ([#122](https://github.com/anatolykoptev/go-panel/issues/122)) ([1b73982](https://github.com/anatolykoptev/go-panel/commit/1b73982a1ae6171008a807ce17c923c31abc42f4))

## [0.22.2](https://github.com/anatolykoptev/go-panel/compare/v0.22.1...v0.22.2) (2026-08-20)


### Added

* **resource:** add SingleRow mode for single-row resources ([#111](https://github.com/anatolykoptev/go-panel/issues/111)) ([eb2a57a](https://github.com/anatolykoptev/go-panel/commit/eb2a57ab0ee49d0b4524a6249f92bdf4ff5885f2))
* **shell:** light theme + toggle, dark stays the default ([#113](https://github.com/anatolykoptev/go-panel/issues/113)) ([fc801f3](https://github.com/anatolykoptev/go-panel/commit/fc801f3c1732a037acc27c5eb9d7fb6cf8e1ad29))
* **writer:** add AfterSave/AfterDelete hooks ([#109](https://github.com/anatolykoptev/go-panel/issues/109)) ([56804d2](https://github.com/anatolykoptev/go-panel/commit/56804d2968b6ab3a107f17dbdde228c1e5f8db8d))
* **writer:** add Delete capability ([#107](https://github.com/anatolykoptev/go-panel/issues/107)) ([5770822](https://github.com/anatolykoptev/go-panel/commit/5770822ee8caddc18dc746b0a007c994782a2ef3))
* **writer:** add PresetValues for scoped create with foreign keys ([#108](https://github.com/anatolykoptev/go-panel/issues/108)) ([aa864e1](https://github.com/anatolykoptev/go-panel/commit/aa864e129eee0ec7b9b07f5239af86a03bcefce1))
* **writer:** add RedirectAfterSave/RedirectAfterDelete ([#110](https://github.com/anatolykoptev/go-panel/issues/110)) ([45a7536](https://github.com/anatolykoptev/go-panel/commit/45a75364ac3e8370af758ac6742cd33e7094c724))


### Fixed

* **resource:** make sidebar grouping independent of registration order ([#120](https://github.com/anatolykoptev/go-panel/issues/120)) ([69e1040](https://github.com/anatolykoptev/go-panel/commit/69e104075fb75cf673374d651dab5bcf0d305357))
* **resource:** make the lint gate green again — sanitise a tainted log id, split three oversized funcs ([#118](https://github.com/anatolykoptev/go-panel/issues/118)) ([8b006b5](https://github.com/anatolykoptev/go-panel/commit/8b006b56a51e278c48fc0413eb5f56321706ba23))

## [0.22.1](https://github.com/anatolykoptev/go-panel/compare/v0.22.0...v0.22.1) (2026-07-21)


### Added

* **resource:** add Relation struct and resolveRelations (P1 core, TDD) ([#103](https://github.com/anatolykoptev/go-panel/issues/103)) ([ed2838a](https://github.com/anatolykoptev/go-panel/commit/ed2838a8398dd5881d045449e6c437d8b82041ce))
* **resource:** promote CrossLinkCell with fixed href escaping (XSS) ([#102](https://github.com/anatolykoptev/go-panel/issues/102)) ([5871c4f](https://github.com/anatolykoptev/go-panel/commit/5871c4f6f57be8146ff2f4b2fe6678d35c6fb2ff))
* **resource:** wire resolveRelations hook in makeListHandler (P1 Phase 3a) ([#104](https://github.com/anatolykoptev/go-panel/issues/104)) ([4fd3d6d](https://github.com/anatolykoptev/go-panel/commit/4fd3d6db39ac08c7c03d0babc2b8f8a0a4b3f3a5))

## [0.20.2](https://github.com/anatolykoptev/go-panel/compare/v0.20.1...v0.20.2) (2026-07-21)


### Added

* **mcp:** add mcp package — auto-expose Resources as MCP tools ([957d0ea](https://github.com/anatolykoptev/go-panel/commit/957d0eaac18e282bca2a7b45c217a33affb539d8))


### Fixed

* **mcp:** replace invalid jsonschema WORD= tags with descriptions ([#98](https://github.com/anatolykoptev/go-panel/issues/98)) ([e0baac3](https://github.com/anatolykoptev/go-panel/commit/e0baac32cb9f352605bd06130cc42da9c40a3080))


### Changed

* adopt go-mcpserver v0.18.0 Serve single-entry API ([#99](https://github.com/anatolykoptev/go-panel/issues/99)) ([9b66acb](https://github.com/anatolykoptev/go-panel/commit/9b66acb1abce0382131ac90cfb8a0f6872e9dca0))

## [0.20.1](https://github.com/anatolykoptev/go-panel/compare/v0.20.0...v0.20.1) (2026-07-14)


### Fixed

* **auth,identity,shell,resource:** harden security and fix gaps found in review ([0feec32](https://github.com/anatolykoptev/go-panel/commit/0feec32e3ab00a034a3749aa7c0318174179bbc7))
* **auth,identity,shell,resource:** security hardening from review ([7738187](https://github.com/anatolykoptev/go-panel/commit/7738187b7e4401411b373b3c5550f69db86d92d3))
* **auth:** avoid gosec G120 by using r.Form.Get in verifyMFA ([809c9a9](https://github.com/anatolykoptev/go-panel/commit/809c9a90b855da70f3d6f7d55603b061baf7f66e))

## [0.20.0](https://github.com/anatolykoptev/go-panel/compare/v0.19.1...v0.20.0) (2026-07-14)


### ⚠ BREAKING CHANGES

* **auth:** fail-closed session recheck, harden safeReturnURL, no hardcoded example key
* **auth:** NewBcryptTOTPAuth now requires a RateLimiter and a usable TOTPRate when Store implements TOTPStore, and a usable LoginRate whenever RateLimiter is configured.
* render.Component is removed. It had no callers in go-panel or its pinned consumers (go-grad v0.19.1, go-job v0.16.0) at the time of removal — this only matters the next time either bumps its go-panel dependency past this release.

### Added

* **auth:** wire MFA login (Arc A P5) ([46b5b08](https://github.com/anatolykoptev/go-panel/commit/46b5b08e981500e9562573a356c5d2c382589b1d))
* Panel.MountAction + Panel.RenderError, dedup CSRF helpers ([#91](https://github.com/anatolykoptev/go-panel/issues/91)) ([a355d06](https://github.com/anatolykoptev/go-panel/commit/a355d065626b6a6a89932788503f9e03fe032a3a))


### Fixed

* **auth:** fail-closed session recheck, harden safeReturnURL, no hardcoded example key ([0f0d1d9](https://github.com/anatolykoptev/go-panel/commit/0f0d1d910fd231d1b45a6d063f47013f28d6d7d3))


### Changed

* Arc C API cleanliness — baseAuthenticator, panic docs, Example tests, drop render.Component ([#89](https://github.com/anatolykoptev/go-panel/issues/89)) ([409e481](https://github.com/anatolykoptev/go-panel/commit/409e481abd2dc0112683e8c5ab6271e48138b865))

## [0.19.1](https://github.com/anatolykoptev/go-panel/compare/v0.19.0...v0.19.1) (2026-07-12)


### Bug Fixes

* unify pm7-card surface token, scope table link color, drop hardcoded hex ([#87](https://github.com/anatolykoptev/go-panel/issues/87)) ([905c4d6](https://github.com/anatolykoptev/go-panel/commit/905c4d63bcd14e2a8facb167a1cbb79735586833))

## [0.19.0](https://github.com/anatolykoptev/go-panel/compare/v0.18.0...v0.19.0) (2026-07-11)


### Features

* **auth:** fail-closed login RateLimiter hook + LoginHandler decomposition + login-outcome metrics ([#82](https://github.com/anatolykoptev/go-panel/issues/82)) ([f00d338](https://github.com/anatolykoptev/go-panel/commit/f00d3386c75d3110fbe01e89d45f5c35fc9f0548))
* **auth:** observable session-recheck degrade + RevocationFailClosed opt-in ([#80](https://github.com/anatolykoptev/go-panel/issues/80)) ([890606a](https://github.com/anatolykoptev/go-panel/commit/890606ad90749a8078c036a1d44f31e81bc6a346))
* **auth:** server-rendered TOTP enrollment UI (enroll/QR/confirm/disable/regen) ([#85](https://github.com/anatolykoptev/go-panel/issues/85)) ([00404bf](https://github.com/anatolykoptev/go-panel/commit/00404bfb4cd98aa88e1dec6087dc479eaeb58a9f))
* **tenant:** wire tenant resolution + fail-closed authz seam (P1a) ([#86](https://github.com/anatolykoptev/go-panel/issues/86)) ([b8da0b2](https://github.com/anatolykoptev/go-panel/commit/b8da0b2c2eeb4793b102a0c404889a2d6608cb99))

## [0.18.0](https://github.com/anatolykoptev/go-panel/compare/v0.17.0...v0.18.0) (2026-07-10)


### Features

* **resource:** MountPage for custom auth-wrapped admin pages ([#77](https://github.com/anatolykoptev/go-panel/issues/77)) ([a8ab1aa](https://github.com/anatolykoptev/go-panel/commit/a8ab1aa59c982957df30953c45dff90546e0421a))

## [0.17.0](https://github.com/anatolykoptev/go-panel/compare/v0.16.0...v0.17.0) (2026-07-10)


### Features

* **resource:** typed SaveError surfaces domain Save failures as form validation ([#75](https://github.com/anatolykoptev/go-panel/issues/75)) ([671133d](https://github.com/anatolykoptev/go-panel/commit/671133d5e3675131b83287d4a17abea6cb91f108))

## [0.16.0](https://github.com/anatolykoptev/go-panel/compare/v0.15.0...v0.16.0) (2026-07-08)


### ⚠ BREAKING CHANGES

* removed resource.Perms, resource.ReadAny, Resource.Perms, and Writer.WriteAny. Consumers setting Perms: resource.ReadAny should delete the line (no-op, behaviour unchanged); consumers relying on WriteAny=true for write access should set Resource.RequiredRole to the role they intended to require, or leave it empty for the previous "any authenticated operator" behaviour.

### Bug Fixes

* council-2026-07 findings — FieldDateTime + Validate hook, remove inert Perms, list-error hygiene, limiter outcome ([#71](https://github.com/anatolykoptev/go-panel/issues/71)) ([4ca0e1a](https://github.com/anatolykoptev/go-panel/commit/4ca0e1a7d550f33524a90016e892f15738854715))

## [0.15.0](https://github.com/anatolykoptev/go-panel/compare/v0.14.0...v0.15.0) (2026-07-07)


### Features

* **email:** From display name, encoded subjects, Date + Message-ID headers ([#69](https://github.com/anatolykoptev/go-panel/issues/69)) ([e87b7fb](https://github.com/anatolykoptev/go-panel/commit/e87b7fb594ee5ace40ba8e1d7cf1d6647549ca64))

## [0.14.0](https://github.com/anatolykoptev/go-panel/compare/v0.13.3...v0.14.0) (2026-07-07)


### Features

* add Reply-To support to identity/email via extension interface ([#67](https://github.com/anatolykoptev/go-panel/issues/67)) ([357efee](https://github.com/anatolykoptev/go-panel/commit/357efee26d986229d87a690f08dfb6b4a56996b7))

## [0.13.3](https://github.com/anatolykoptev/go-panel/compare/v0.13.2...v0.13.3) (2026-07-05)


### Bug Fixes

* **resource:** first-click pagination + Load-more append mode ([#65](https://github.com/anatolykoptev/go-panel/issues/65)) ([b0092bb](https://github.com/anatolykoptev/go-panel/commit/b0092bb5764eb0fffa9bfb4b080522e96c287203))

## [0.13.2](https://github.com/anatolykoptev/go-panel/compare/v0.13.1...v0.13.2) (2026-06-28)


### Bug Fixes

* **shell:** raise sidebar group-label contrast to WCAG 3:1 ([#58](https://github.com/anatolykoptev/go-panel/issues/58)) ([1f3a8aa](https://github.com/anatolykoptev/go-panel/commit/1f3a8aad2e895cf9bb2ad263e2bb2e282d69867b))

## [0.13.1](https://github.com/anatolykoptev/go-panel/compare/v0.13.0...v0.13.1) (2026-06-28)


### Bug Fixes

* **shell:** pin sidebar to viewport height (sticky, 100vh) ([#55](https://github.com/anatolykoptev/go-panel/issues/55)) ([333cc5b](https://github.com/anatolykoptev/go-panel/commit/333cc5bddf1726326c781382f0c0226a33f79930))
* **shell:** regenerate styles_templ.go with sticky sidebar (close [#55](https://github.com/anatolykoptev/go-panel/issues/55) gap) ([#56](https://github.com/anatolykoptev/go-panel/issues/56)) ([1c1e1e6](https://github.com/anatolykoptev/go-panel/commit/1c1e1e635cb6ea6f69e69f0bae2987df3e339a75))

## [0.13.0](https://github.com/anatolykoptev/go-panel/compare/v0.12.0...v0.13.0) (2026-06-28)


### Features

* **resource,shell:** nav-filter via HasRole + profile block (P3.3 + P7) ([798508d](https://github.com/anatolykoptev/go-panel/commit/798508d0906e625fc217f0d0d6c617aceb58939e))
* **resource,shell:** nav-filter via HasRole + profile block (P3.3 + P7) ([#51](https://github.com/anatolykoptev/go-panel/issues/51)) ([798508d](https://github.com/anatolykoptev/go-panel/commit/798508d0906e625fc217f0d0d6c617aceb58939e))

## [0.12.0](https://github.com/anatolykoptev/go-panel/compare/v0.11.0...v0.12.0) (2026-06-28)


### Features

* **resource,shell:** nav-filter via HasRole + profile block (P3.3 + P7) ([#49](https://github.com/anatolykoptev/go-panel/issues/49)) ([c27ba02](https://github.com/anatolykoptev/go-panel/commit/c27ba02aceca175b1c105b55168264a8072e9c4e))

## [0.11.0](https://github.com/anatolykoptev/go-panel/compare/v0.10.0...v0.11.0) (2026-06-28)


### Features

* **shell:** collapsible nav groups with cookie persistence ([#44](https://github.com/anatolykoptev/go-panel/issues/44)) ([675770b](https://github.com/anatolykoptev/go-panel/commit/675770b449beb5f05aaed10ebaed2dc843f70fd1))
* **shell:** mobile off-canvas drawer (pm7-borrowed overlay) ([#46](https://github.com/anatolykoptev/go-panel/issues/46)) ([7d48a9f](https://github.com/anatolykoptev/go-panel/commit/7d48a9fbd53b7271538008a154bf467feabd246b))
* **shell:** one-level nested submenus via NavItem.Children ([#45](https://github.com/anatolykoptev/go-panel/issues/45)) ([56c8f58](https://github.com/anatolykoptev/go-panel/commit/56c8f58ed8345e78af4477d53439d1860157a2ce))

## [0.10.0](https://github.com/anatolykoptev/go-panel/compare/v0.9.0...v0.10.0) (2026-06-27)


### Features

* **shell:** working icon-rail collapse (cookie-SSR, no flash) + live nav badges ([#39](https://github.com/anatolykoptev/go-panel/issues/39)) ([d44aa63](https://github.com/anatolykoptev/go-panel/commit/d44aa6374fce919a47387555c654dfb796418685))

## [0.9.0](https://github.com/anatolykoptev/go-panel/compare/v0.8.0...v0.9.0) (2026-06-27)


### Features

* **admin:** delegated drag-and-drop + keyboard reorder for .gd-sortable lists ([#36](https://github.com/anatolykoptev/go-panel/issues/36)) ([4a14423](https://github.com/anatolykoptev/go-panel/commit/4a144232c505af3679aa4955c7e0412d2e20be04))
* **auth:** account store for multi-user auth (PgxAccountStore + bcrypt) ([#34](https://github.com/anatolykoptev/go-panel/issues/34)) ([67718c2](https://github.com/anatolykoptev/go-panel/commit/67718c279751c18d7f62006562a45f379e012baf))
* **auth:** BcryptTOTPAuth — multi-user bcrypt login + roles + live revocation ([#35](https://github.com/anatolykoptev/go-panel/issues/35)) ([bb752d8](https://github.com/anatolykoptev/go-panel/commit/bb752d819d8ee76530a365f935bed413637c0556))
* **resource:** Panel.AddNav for non-resource sidebar entries ([#28](https://github.com/anatolykoptev/go-panel/issues/28)) ([7e2bd3f](https://github.com/anatolykoptev/go-panel/commit/7e2bd3fba182bf1cf2ed42dca2973e29329af2a6))
* **resource:** Panel.RenderPage/RenderPageHTML for bespoke pages in the shell ([#29](https://github.com/anatolykoptev/go-panel/issues/29)) ([2d3cda0](https://github.com/anatolykoptev/go-panel/commit/2d3cda0273be1661087a5bddd595cda8ee763f5d))
* **resource:** Resource.Detail — auto-mounted per-record detail pages ([#31](https://github.com/anatolykoptev/go-panel/issues/31)) ([1ba6aae](https://github.com/anatolykoptev/go-panel/commit/1ba6aae47d9c831f7391ce0ee000d7acb48d4bb1))
* **resource:** reusable Detailer Show-view + status-chip CSS + Width/Align wiring ([#27](https://github.com/anatolykoptev/go-panel/issues/27)) ([8374fd8](https://github.com/anatolykoptev/go-panel/commit/8374fd8feb60383e2c7b16336e1dfc034cb01691))
* **semantic:** new package — reusable multi-table pgvector semantic search ([#25](https://github.com/anatolykoptev/go-panel/issues/25)) ([9256db9](https://github.com/anatolykoptev/go-panel/commit/9256db954e7e972ee845d577d7de7fad2fc86ddd))


### Bug Fixes

* **auth:** render pm7 login page by default + finish password toggle ([#32](https://github.com/anatolykoptev/go-panel/issues/32)) ([e4cfdb4](https://github.com/anatolykoptev/go-panel/commit/e4cfdb436679788aaa3029b6aec501f0dc9cb3bf))
* **resource:** preserve filter query params in pagination links ([#37](https://github.com/anatolykoptev/go-panel/issues/37)) ([c7f3b1e](https://github.com/anatolykoptev/go-panel/commit/c7f3b1e347b234d7277f22b45cb7b820a5bf4ea8))
* **resource:** RenderPage sets admin security headers (CSP) like resource pages ([#30](https://github.com/anatolykoptev/go-panel/issues/30)) ([0d978fc](https://github.com/anatolykoptev/go-panel/commit/0d978fcf8201fffffbe3892c8e1cc20c1f5c8d24))
* **shell:** hide inactive password-toggle icon ([#33](https://github.com/anatolykoptev/go-panel/issues/33)) ([363288a](https://github.com/anatolykoptev/go-panel/commit/363288ad6ba26d63a8cf2dd5c634b9cbc1dde018))

## [0.8.0](https://github.com/anatolykoptev/go-panel/compare/v0.7.0...v0.8.0) (2026-06-17)


### Features

* **locale:** locale axis for go-panel i18n (ADR-003 Phase 1a) ([#22](https://github.com/anatolykoptev/go-panel/issues/22)) ([54861ed](https://github.com/anatolykoptev/go-panel/commit/54861ed5b5e5420fdfc769a605a97b7db21f1f4d))
* **resource:** translatable fields + per-locale admin forms ([#24](https://github.com/anatolykoptev/go-panel/issues/24)) ([67c3ce8](https://github.com/anatolykoptev/go-panel/commit/67c3ce8aa8a3666b00981cc58a9ae99db920d4ac))

## [0.7.0](https://github.com/anatolykoptev/go-panel/compare/v0.6.0...v0.7.0) (2026-06-17)


### Features

* **identity:** batteries-included — Redis RateLimiter + Prometheus Observer subpackages ([#18](https://github.com/anatolykoptev/go-panel/issues/18)) ([91d2a91](https://github.com/anatolykoptev/go-panel/commit/91d2a9134d2e5f38079e0b4c94d0d78f92eac0e3))

## [0.6.0](https://github.com/anatolykoptev/go-panel/compare/v0.5.0...v0.6.0) (2026-06-17)


### Features

* **identity:** forward raw email to UserStore for plaintext contact storage ([#15](https://github.com/anatolykoptev/go-panel/issues/15)) ([5ff2ab9](https://github.com/anatolykoptev/go-panel/commit/5ff2ab94ca78dd6b8f709baafa04c6293a4c9105))
* **identity:** observability seam — Observer interface on Config, RED outcomes at auth handlers ([#14](https://github.com/anatolykoptev/go-panel/issues/14)) ([034fc40](https://github.com/anatolykoptev/go-panel/commit/034fc40d6c61d2936c9fa530133f732a280369f5))
* **identity:** public-auth framework — magic-link, Redis sessions, pepper-keyed provider-uid, exact-host cookies ([#13](https://github.com/anatolykoptev/go-panel/issues/13)) ([3304c40](https://github.com/anatolykoptev/go-panel/commit/3304c40520022eb98aad54c259f55057d2d277da))

## [0.4.1](https://github.com/anatolykoptev/go-panel/compare/v0.4.0...v0.4.1) (2026-06-11)


### Bug Fixes

* **tenant:** PathResolver honours slug only after the literal 'tenant' segment ([#8](https://github.com/anatolykoptev/go-panel/issues/8)) ([9d6dc81](https://github.com/anatolykoptev/go-panel/commit/9d6dc81bc21a41aa88f9701288a8f3cb1c704db8))

## [0.4.0](https://github.com/anatolykoptev/go-panel/compare/v0.3.0...v0.4.0) (2026-06-11)


### Features

* **auth,resource:** per-login session nonce + HMACKey floor + dynamic Select options ([#6](https://github.com/anatolykoptev/go-panel/issues/6)) ([b9da954](https://github.com/anatolykoptev/go-panel/commit/b9da954eae07f5f4dcea3d12ea702a51c65e0d5e))

## [0.3.0](https://github.com/anatolykoptev/go-panel/compare/v0.2.0...v0.3.0) (2026-06-11)


### Features

* **resource:** Phase 2 Writer — create/edit forms + CSRF ([#3](https://github.com/anatolykoptev/go-panel/issues/3)) ([937a2ee](https://github.com/anatolykoptev/go-panel/commit/937a2eeb2d332b0a2c25f925dbb67a82a8f7ed3f))

## [0.2.0](https://github.com/anatolykoptev/go-panel/compare/v0.1.0...v0.2.0) (2026-06-06)


### Features

* go-panel foundations — shell/auth/render/tenant/resource kit on go-kit, templ+htmx ([22cc109](https://github.com/anatolykoptev/go-panel/commit/22cc109418cca4bdac5dd6a1215ee5cd1f73691e))


### Bug Fixes

* depend on go-kit/admintable instead of a local duplicate ([#1](https://github.com/anatolykoptev/go-panel/issues/1)) ([aa013d6](https://github.com/anatolykoptev/go-panel/commit/aa013d6afe0ab043fa4625c53ad604c092d0dc58))
