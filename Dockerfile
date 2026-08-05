FROM golang:1.25-alpine AS build

ARG SERVICE
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN test -n "${SERVICE}" && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/matchmind-service "./cmd/${SERVICE}"

FROM alpine:3.22

RUN addgroup -S matchmind && adduser -S -G matchmind matchmind
USER matchmind
COPY --from=build /out/matchmind-service /usr/local/bin/matchmind-service

ENTRYPOINT ["/usr/local/bin/matchmind-service"]
