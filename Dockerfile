# syntax=docker/dockerfile:1

# Debian trixie (glibc 2.41), deliberately not bookworm (2.36): better-sqlite3
# ships a prebuilt linux binary linked against GLIBC_2.38, so on bookworm it
# dies at require() with ERR_DLOPEN_FAILED and the only way out is compiling it
# from source -- python3/make/g++ in the image plus a node-gyp invocation that
# has to pass --force_build=1, because binding.gyp turns every target into
# 'type': 'none' when a prebuild is already present. A new enough libc makes the
# shipped prebuild simply load, and all of that disappears.
FROM node:22-trixie-slim AS base
WORKDIR /app

# Every install below passes --ignore-scripts. Nothing in this tree needs a
# lifecycle script -- better-sqlite3 uses its bundled prebuild, and vite,
# esbuild and tailwind's oxide ship native binaries as platform-specific
# optional dependencies. It also stops npm running its *implicit* `node-gyp
# rebuild` on better-sqlite3, which needs python3 and make even when the gyp
# targets resolve to nothing to build.

# ---- deps: full install, devDependencies included (tsc, vite) -------------
FROM base AS deps
# Only the manifests, so this layer is reused until a dependency actually
# changes -- editing source does not reinstall node_modules.
COPY package.json package-lock.json ./
COPY packages/shared/package.json packages/shared/package.json
COPY packages/server/package.json packages/server/package.json
COPY packages/dashboard/package.json packages/dashboard/package.json
RUN npm ci --ignore-scripts

# ---- build: compile the server and the dashboard --------------------------
FROM deps AS build
COPY . .
RUN npm run build

# ---- prod-deps: runtime dependencies only ---------------------------------
# A separate install rather than pruning the one above, so devDependencies
# never exist in a layer the runtime image copies from.
FROM base AS prod-deps
COPY package.json package-lock.json ./
COPY packages/shared/package.json packages/shared/package.json
COPY packages/server/package.json packages/server/package.json
COPY packages/dashboard/package.json packages/dashboard/package.json
RUN npm ci --omit=dev --ignore-scripts

# ---- runtime: compiled output + production node_modules -------------------
FROM base AS runtime
ENV NODE_ENV=production

COPY --from=prod-deps /app/node_modules ./node_modules
COPY --from=prod-deps /app/package.json ./package.json
# @breckr/shared is types-only and every import of it erases at compile time,
# so nothing here is needed at runtime -- copied anyway so the workspace layout
# stays valid instead of leaving a dangling node_modules symlink.
COPY --from=build /app/packages/shared/package.json ./packages/shared/package.json
COPY --from=build /app/packages/shared/src ./packages/shared/src
COPY --from=build /app/packages/server/package.json ./packages/server/package.json
COPY --from=build /app/packages/server/dist ./packages/server/dist
COPY --from=build /app/packages/dashboard/dist ./packages/dashboard/dist

# Cosmetic -- what actually gets published is set in docker-compose.yml, which
# reads PORT from .env.
EXPOSE 3000

CMD ["node", "packages/server/dist/index.js"]
