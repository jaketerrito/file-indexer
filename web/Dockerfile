# Build stage: install all deps, build client + SSR server bundles.
FROM node:26.5-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build
# Drop devDependencies; dist/server externalizes runtime deps (react, srvx,
# connect, ...) so production node_modules must ship in the final image.
RUN npm prune --omit=dev

# Runtime stage: static client assets + SSR fetch handler served by srvx.
FROM node:26.5-alpine
# npm is build-time only; it vendors a vulnerable undici (CVE-2026-12151),
# so remove it from the runtime image entirely. Only `node` is needed to
# run srvx.
RUN rm -rf /usr/local/lib/node_modules /usr/local/bin/npm /usr/local/bin/npx
WORKDIR /app
ENV NODE_ENV=production
COPY --from=build /app/package.json ./package.json
COPY --from=build /app/node_modules ./node_modules
COPY --from=build /app/dist ./dist
USER 10001:10001
EXPOSE 3000
CMD ["./node_modules/.bin/srvx", "serve", "--prod", "--dir=.", "--static=dist/client", "--entry=dist/server/server.js"]
