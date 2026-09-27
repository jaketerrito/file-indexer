# Changelog

## [0.4.0](https://github.com/jaketerrito/file-indexer/compare/v0.3.0...v0.4.0) (2026-09-27)


### Features

* **deploy:** configure crawler and preview-gc as production CronJobs ([#150](https://github.com/jaketerrito/file-indexer/issues/150)) ([79823b0](https://github.com/jaketerrito/file-indexer/commit/79823b0e4bce48f22c7b459d1c1c8634902ee829))


### Bug Fixes

* **photos:** stop redundant refetches and index taken_at sort ([#149](https://github.com/jaketerrito/file-indexer/issues/149)) ([92a223b](https://github.com/jaketerrito/file-indexer/commit/92a223bf5c198dbfd1c25384f41e7d48b5c3c21d))
* **web:** responsive photo grid with at least 5 columns on mobile ([#147](https://github.com/jaketerrito/file-indexer/issues/147)) ([3e9c9bd](https://github.com/jaketerrito/file-indexer/commit/3e9c9bdcb5eff4f5305d86ce65c131e8de2fae17))

## [0.3.0](https://github.com/jaketerrito/file-indexer/compare/v0.2.0...v0.3.0) (2026-09-27)


### Features

* **deploy:** Replace=true sync option for the migrate Job ([#135](https://github.com/jaketerrito/file-indexer/issues/135)) ([5036b93](https://github.com/jaketerrito/file-indexer/commit/5036b93ef7b22e142fa0b935364faafd513e027e))
* **files:** open files inline in browser from the table actions ([#137](https://github.com/jaketerrito/file-indexer/issues/137)) ([aa29e66](https://github.com/jaketerrito/file-indexer/commit/aa29e6666a58cc7994c4c1637e8edd8f00ec0fa3))
* **indexer:** heartbeat /healthz and Prometheus lag metrics for index workers ([#138](https://github.com/jaketerrito/file-indexer/issues/138)) ([aba1a7b](https://github.com/jaketerrito/file-indexer/commit/aba1a7b9661118b8a7dd665ecda18b4820732a3a)), closes [#117](https://github.com/jaketerrito/file-indexer/issues/117)
* move files to other folders ([#141](https://github.com/jaketerrito/file-indexer/issues/141)) ([163ef62](https://github.com/jaketerrito/file-indexer/commit/163ef6293d22d41af0fc694d07a599c0c2003ff8))
* photos page rework with takenAt sort and day separators ([#146](https://github.com/jaketerrito/file-indexer/issues/146)) ([8549e5e](https://github.com/jaketerrito/file-indexer/commit/8549e5ecb3be9ebbe8b30a092837f465c66ae528))
* **web:** inline file viewer on file page ([#145](https://github.com/jaketerrito/file-indexer/issues/145)) ([6755d07](https://github.com/jaketerrito/file-indexer/commit/6755d0713ca9d66ee5801780232f8341a8224af6))
* **web:** redesign file browser UI ([#139](https://github.com/jaketerrito/file-indexer/issues/139)) ([#144](https://github.com/jaketerrito/file-indexer/issues/144)) ([5ce57eb](https://github.com/jaketerrito/file-indexer/commit/5ce57eb04629e428ecf3afae4d50874830c4addf))


### Bug Fixes

* replace MinIO with RustFS as the S3 backend ([#143](https://github.com/jaketerrito/file-indexer/issues/143)) ([6516415](https://github.com/jaketerrito/file-indexer/commit/65164155b804f2220a710b2b943783d825c7c38c))

## [0.2.0](https://github.com/jaketerrito/file-indexer/compare/v0.1.2...v0.2.0) (2026-09-24)


### Features

* **ci:** release-please flow; overlay tags bumped in the release PR ([#128](https://github.com/jaketerrito/file-indexer/issues/128)) ([ae11e67](https://github.com/jaketerrito/file-indexer/commit/ae11e67ce10d143da9a4f5430c6ec96683eb6989))


### Bug Fixes

* **deploy:** pin ServiceAccount to sync-wave -2, before migrate ([#125](https://github.com/jaketerrito/file-indexer/issues/125)) ([ba4a177](https://github.com/jaketerrito/file-indexer/commit/ba4a177c20e3a5e9c2d6e6ce4a5e05bdee7963b2))
