# Production build. One image per service, selected by the SERVICE build arg.
#   docker build -f deploy/Dockerfile --build-arg SERVICE=auth -t reporangler/auth .
# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS build
ARG SERVICE=auth
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/service ./cmd/${SERVICE}

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /out/service /usr/local/bin/service
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/service"]
