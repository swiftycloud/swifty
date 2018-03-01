FROM golang:latest

WORKDIR /home
RUN go get github.com/go-sql-driver/mysql
RUN go get gopkg.in/mgo.v2
COPY swy-wdog /usr/bin/
RUN chmod +x /usr/bin/swy-wdog
COPY runner.go /go/src/swyrunner/
COPY queue.go /go/src/xqueue/

EXPOSE 8687

#
# Run wdog daemon inside
CMD [ "/usr/bin/swy-wdog" ]
