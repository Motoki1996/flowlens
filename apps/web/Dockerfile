# syntax=docker/dockerfile:1

# ---- deps: install dependencies for a cacheable layer -----------------
FROM node:22-alpine AS deps
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci

# ---- build: compile the standalone Next.js output ----------------------
FROM node:22-alpine AS build
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .

# NEXT_PUBLIC_* vars are inlined into the client bundle at build time, so
# they cannot be changed on a running container. The published image is
# therefore built without one: empty means the browser calls this server on
# its own origin and the Next.js rewrites proxy through to the API (see
# next.config.ts), which is what lets one image serve any hostname.
# Pass it only when building a web image for an API on its own hostname.
ARG NEXT_PUBLIC_API_BASE_URL=""
ENV NEXT_PUBLIC_API_BASE_URL=$NEXT_PUBLIC_API_BASE_URL

RUN npm run build

# ---- runner: minimal production image ----------------------------------
FROM node:22-alpine AS runner
WORKDIR /app
ENV NODE_ENV=production
ENV PORT=4000
ENV HOSTNAME=0.0.0.0

# `output: "standalone"` in next.config.ts traces only the files the server
# needs, so devDependencies never make it into this image.
COPY --from=build /app/public ./public
COPY --from=build /app/.next/standalone ./
COPY --from=build /app/.next/static ./.next/static

EXPOSE 4000
USER node
CMD ["node", "server.js"]
