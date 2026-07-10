FROM node:24.18.0-alpine3.24@sha256:a0b9bf06e4e6193cf7a0f58816cc935ff8c2a908f81e6f1a95432d679c54fbfd AS node-base

# Build stage: install all deps, build client + SSR server bundles.
FROM node-base AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build
RUN npm prune --omit=dev

FROM node-base
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
