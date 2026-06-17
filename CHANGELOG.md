# Changelog

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
