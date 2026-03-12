# syntax=docker/dockerfile:1.7

FROM --platform=$BUILDPLATFORM golang:1.26-alpine3.23 AS build

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY api ./api
COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /out/internctl ./cmd/internctl

FROM alpine:3.23

RUN apk add --no-cache ca-certificates

COPY --from=build /out/internctl /usr/local/bin/internctl

ENTRYPOINT ["internctl"]
