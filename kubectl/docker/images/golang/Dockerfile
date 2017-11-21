FROM golang:latest

WORKDIR /home

COPY swy-wdog /usr/bin/
RUN chmod +x /usr/bin/swy-wdog
COPY main.go /go/src/main/

EXPOSE 8687

#
# Run wdog daemon inside
CMD [ "/usr/bin/swy-wdog" ]
