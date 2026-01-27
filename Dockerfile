FROM golang:bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/mwarchiver .

FROM debian:bookworm-slim

RUN apt-get update \
  && apt-get install -y --no-install-recommends bash ca-certificates curl zip jq \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=build /out/mwarchiver /usr/local/bin/mwarchiver
COPY scripts/entrypoint.sh /usr/local/bin/entrypoint.sh

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
