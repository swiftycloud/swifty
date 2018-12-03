FROM golang:latest

WORKDIR /home
RUN go get github.com/go-sql-driver/mysql
RUN go get gopkg.in/mgo.v2
RUN go get golang.org/x/crypto/bcrypt
RUN go get github.com/aws/aws-sdk-go
COPY swy-wdog /usr/bin/
COPY swy-runner /usr/bin/
COPY go-sca /usr/bin/
RUN chmod +x /usr/bin/swy-wdog /usr/bin/go-sca
COPY runner.go /go/src/swyrunner/
COPY body.go /go/src/swyrunner/body
COPY lib.go /go/src/swifty/
COPY queue.go /go/src/xqueue/
COPY runner-go.sh /usr/bin/start_runner.sh
COPY builder.sh /usr/bin/build_runner.sh
RUN chmod +x /usr/bin/start_runner.sh /usr/bin/build_runner.sh

EXPOSE 8687

#
# Run wdog daemon inside
CMD [ "/usr/bin/swy-wdog" ]
