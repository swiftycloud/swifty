FROM golang:latest

WORKDIR /home
RUN go get github.com/go-sql-driver/mysql
RUN go get gopkg.in/mgo.v2
RUN go get golang.org/x/crypto/bcrypt
RUN go get github.com/aws/aws-sdk-go
COPY swy-wdog /usr/bin/
COPY swy-runner /usr/bin/
RUN chmod +x /usr/bin/swy-wdog
COPY runner.go /go/src/swyrunner/
COPY lib.go /go/src/swifty/
COPY queue.go /go/src/xqueue/

EXPOSE 8687

#
# Run wdog daemon inside
CMD [ "/usr/bin/swy-wdog" ]
