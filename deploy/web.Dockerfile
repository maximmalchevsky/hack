

FROM node:22-alpine AS builder
WORKDIR /src

RUN corepack enable && corepack prepare pnpm@9.12.0 --activate

COPY package.json pnpm-lock.yaml* ./
RUN pnpm install --frozen-lockfile || pnpm install

COPY . .

ARG PUBLIC_API_URL
ARG PUBLIC_WS_URL
ENV PUBLIC_API_URL=$PUBLIC_API_URL
ENV PUBLIC_WS_URL=$PUBLIC_WS_URL

RUN pnpm build

FROM node:22-alpine AS runtime
WORKDIR /app
ENV NODE_ENV=production

RUN corepack enable && corepack prepare pnpm@9.12.0 --activate

COPY --from=builder /src/build /app/build
COPY --from=builder /src/package.json /app/package.json
COPY --from=builder /src/node_modules /app/node_modules

EXPOSE 3000
ENV PORT=3000
ENV HOST=0.0.0.0

USER node
CMD ["node", "build"]
