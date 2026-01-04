FROM golang:1.24-alpine AS build

ENV CGO_ENABLED=0

WORKDIR /build

COPY go.*  ./
COPY *.go ./
COPY migrate ./migrate

RUN go build -o tern

FROM alpine:3.20
COPY --from=build /build/tern /tern
ENTRYPOINT ["/tern"]

