# Development image with air hot-reload. The SERVICE env var selects which
# cmd/ package air builds and runs (see deploy/.air.toml). Source is bind-
# mounted at /app by docker-compose, so edits trigger a rebuild+restart.
FROM golang:1.25-alpine
RUN apk add --no-cache git && go install github.com/air-verse/air@latest
WORKDIR /app
ENV SERVICE=auth
CMD ["air", "-c", "deploy/.air.toml"]
