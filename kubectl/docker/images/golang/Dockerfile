FROM golang:latest

WORKDIR /home

COPY swy-wdog /usr/bin/
RUN chmod +x /usr/bin/swy-wdog
COPY runner.go /go/src/swyrunner/
COPY queue.go /go/src/xqueue/

EXPOSE 8687

#
# Run wdog daemon inside
CMD [ "/usr/bin/swy-wdog" ]
