FROM golang:alpine
RUN apk add git
RUN GO111MODULE=on go get github.com/cucumber/godog/cmd/godog@v0.10.0
ARG TAG=v1.2.22
RUN GO111MODULE=on go get github.com/miko/ghatt/cmd/ghatt@${TAG}
RUN go get github.com/miko/waitforit/v2
ENTRYPOINT ghatt
WORKDIR /ghatt
COPY examples /ghatt/features
CMD "/ghatt/features"

