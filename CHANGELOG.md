# Changelog

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
