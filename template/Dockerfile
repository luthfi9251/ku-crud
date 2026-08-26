# Full-stack build of the kucrud template starter.
#
# Build from the REPO ROOT so the in-repo core/ is in the context (the
# template's go.mod replaces github.com/luthfi9251/kucrud-core =>
# ../core until the module is published):
#
#	docker build -f template/Dockerfile -t kucrud-template .
#	docker run --rm -p 8080:8080 -e KUCRUD_DB_DSN=postgres://... kucrud-template
#
# NOTE: not yet exercised in CI/locally (no docker in the dev env) —
# correct by construction; verify on first use.

# 1. Web app: Vite + React -> static assets.
FROM node:22-alpine AS web
WORKDIR /src/web
COPY template/web/package.json template/web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY template/web/ ./
RUN npm run build

# 2. Go server: static binary against the in-repo core.
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY core/ core/
COPY template/ template/
WORKDIR /src/template
RUN CGO_ENABLED=0 go build -o /out/kucrud-template .

# 3. Minimal runtime: binary + built web assets.
FROM alpine:3.21
RUN adduser -D app && apk add --no-cache ca-certificates
USER app
WORKDIR /app
COPY --from=build /out/kucrud-template ./
COPY --from=web /src/web/dist web/dist
ENV KUCRUD_ADDR=:8080
EXPOSE 8080
ENTRYPOINT ["./kucrud-template"]
