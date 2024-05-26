## Build
FROM golang:1.22 as build

WORKDIR /build

COPY . .

RUN go mod download && go mod verify
RUN CGO_ENABLED=0 go build -v -o app

## Deploy
FROM alpine:latest

WORKDIR /opt/app
COPY --from=build /build/app .

RUN chmod u+x ./app

ENTRYPOINT ["/opt/app/app"]
