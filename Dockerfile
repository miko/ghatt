FROM golang:alpine
RUN GO111MODULE=on go get github.com/cucumber/godog/cmd/godog@v0.10.0
RUN GO111MODULE=on go get github.com/miko/ghatt/cmd/ghatt
RUN GO111MODULE=on go get github.com/miko/waitforit
ENTRYPOINT ghatt
WORKDIR /ghatt
COPY examples /ghatt/features
CMD "/ghatt/features"

