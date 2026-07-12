# Changelog

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
