FROM node:24.20.0-alpine3.24@sha256:e67514e5d0f6c46656005e1b693b2ec9d52e80b641307de684d4a015ba7a4eaf AS node-base

# Build stage: install all deps, build client + SSR server bundles.
FROM node-base AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build
RUN npm prune --omit=dev

FROM node-base
# The pinned alpine3.24 base ships libcrypto3 3.5.7-r0 (CVE-2026-14456, fixed
# in 3.5.8-r0) and node:24-alpine3.24 has no newer build yet; upgrade the
# openssl libs until a rebuilt base absorbs this.
RUN apk add --no-cache --upgrade libcrypto3 libssl3
RUN rm -rf /usr/local/lib/node_modules /usr/local/bin/npm /usr/local/bin/npx
WORKDIR /app
ENV NODE_ENV=production
COPY --from=build /app/package.json ./package.json
COPY --from=build /app/node_modules ./node_modules
COPY --from=build /app/dist ./dist
USER 10001:10001
EXPOSE 3000
# These args mirror the `start` script in package.json -- keep both in sync.
CMD ["node", "node_modules/srvx/bin/srvx.mjs", "serve", "--prod", "--dir=.", "--static=dist/client", "--entry=dist/server/server.js"]
