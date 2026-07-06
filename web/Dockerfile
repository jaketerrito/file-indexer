# Build stage: install all deps, build client + SSR server bundles.
FROM node:22-alpine AS build
WORKDIR /app
RUN corepack enable pnpm
COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY . .
RUN pnpm build
# Drop devDependencies; dist/server externalizes runtime deps (react, srvx,
# connect, ...) so production node_modules must ship in the final image.
RUN pnpm prune --prod

# Runtime stage: static client assets + SSR fetch handler served by srvx.
FROM node:22-alpine
WORKDIR /app
ENV NODE_ENV=production
COPY --from=build /app/package.json ./package.json
COPY --from=build /app/node_modules ./node_modules
COPY --from=build /app/dist ./dist
USER 10001:10001
EXPOSE 3000
CMD ["./node_modules/.bin/srvx", "serve", "--prod", "--dir=.", "--static=dist/client", "--entry=dist/server/server.js"]
