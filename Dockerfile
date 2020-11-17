FROM golang:alpine
RUN apk add git
RUN GO111MODULE=on go get github.com/cucumber/godog/cmd/godog@v0.10.0
RUN go get github.com/miko/waitforit
WORKDIR /usr/local/go/src
RUN git clone https://github.com/miko/ghatt.git
WORKDIR /usr/local/go/src/ghatt
RUN /bin/echo -e "TAG=$(git tag -l|tail -1)" > init.sh
RUN /bin/echo -e "COMMIT=$(git rev-list -1 HEAD)" >> init.sh
#RUN GO111MODULE=on go get -v -u github.com/miko/ghatt/cmd/ghatt
RUN . ./init.sh && echo TAG=$TAG COMM=$COMMIT
RUN . ./init.sh && go build -o /go/bin/ghatt -ldflags "-X main.COMMIT=$COMMIT -X main.TAG=$TAG" ghatt/cmd/ghatt
ENTRYPOINT ghatt
WORKDIR /ghatt
COPY examples /ghatt/features
CMD "/ghatt/features"

