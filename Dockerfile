FROM node:24.21.0-alpine AS web-build
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26.0-alpine AS go-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web-build /src/web/dist ./internal/webapp/dist
ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags='-s -w' -o /out/projecthub-api ./cmd/api

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && addgroup -S -g 10001 projecthub && adduser -S -D -H -u 10001 -G projecthub projecthub && mkdir -p /var/lib/projecthub /run/projecthub-agent && chmod 0755 /run/projecthub-agent && chown projecthub:projecthub /var/lib/projecthub
COPY --from=go-build /out/projecthub-api /usr/local/bin/projecthub-api
USER 10001:10001
ENV PROJECTHUB_LISTEN=0.0.0.0:8080 PROJECTHUB_DB_PATH=/var/lib/projecthub/projecthub.db
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/projecthub-api"]
