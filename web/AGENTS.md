# web/AGENTS.md

React/TS frontend. See `package.json` for scripts.

- `npm ci` before running scripts (not `npm install`); `just lint-web` /
  `just fmt-web` / `just test-web` wrap this.
- Biome is the only linter/formatter — no ESLint/Prettier.
- Generated TS protobuf clients: regenerate via root `just generate`, never
  hand-edit.
