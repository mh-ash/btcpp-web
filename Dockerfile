# syntax=docker/dockerfile:1

FROM golang:1.20-alpine

WORKDIR /app

RUN apk update && \
	apk add --no-cache make

COPY . .
RUN go mod download
RUN make build

RUN apk --no-cache add ca-certificates
RUN apk add chromium
#RUN dpkg -i ./google-chrome-stable_current_amd64.deb

CMD [ "./target/btcpp-web" ]
