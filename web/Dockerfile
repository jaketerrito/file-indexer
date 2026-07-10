# Pinned by digest (like the GitHub Actions in .github/workflows); the tag
# documents what the digest points at. Bump tag + digest together:
#   docker buildx imagetools inspect node:<tag> | grep Digest
FROM node:24.18.0-alpine3.24@sha256:a0b9bf06e4e6193cf7a0f58816cc935ff8c2a908f81e6f1a95432d679c54fbfd AS node-base

# Build stage: install all deps, build client + SSR server bundles.
FROM node-base AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build
# Drop devDependencies; dist/server externalizes runtime deps (react, srvx,
# connect, ...) so production node_modules must ship in the final image.
RUN npm prune --omit=dev

# Runtime stage: static client assets + SSR fetch handler served by srvx.
# npm is build-time only and vendors its own deps (undici et al) that image
# scanners flag, so strip it from the runtime image; only `node` is needed.
# TODO: move to gcr.io/distroless/nodejs26 once it exists (Node 26 goes LTS
# 2026-10) and its base has current openssl; distroless nodejs24 currently
# ships a stale libssl3 that fails the trivy gate.
FROM node-base
RUN rm -rf /usr/local/lib/node_modules /usr/local/bin/npm /usr/local/bin/npx
WORKDIR /app
ENV NODE_ENV=production
COPY --from=build /app/package.json ./package.json
COPY --from=build /app/node_modules ./node_modules
COPY --from=build /app/dist ./dist
USER 10001:10001
EXPOSE 3000
# Invoke the srvx entry module with node directly rather than the
# node_modules/.bin/srvx shebang wrapper.
CMD ["node", "node_modules/srvx/bin/srvx.mjs", "serve", "--prod", "--dir=.", "--static=dist/client", "--entry=dist/server/server.js"]
