FROM golang:alpine as build-env

RUN mkdir /opt/program
WORKDIR /opt/program

RUN apk update
RUN apk add build-base ca-certificates git

COPY go.mod .
COPY go.sum .
RUN go mod download

COPY . .

WORKDIR /opt/program/cmd/socketsrv
RUN go build

# Build minimal image:
FROM alpine
COPY --from=build-env /opt/program/cmd/socketsrv/socketsrv /opt/socketsrv

EXPOSE 8087

WORKDIR /opt
CMD ./socketsrv