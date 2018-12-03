FROM golang:latest

WORKDIR /home
RUN go get github.com/go-sql-driver/mysql && \
	go get gopkg.in/mgo.v2 && \
	go get golang.org/x/crypto/bcrypt && \
	go get github.com/aws/aws-sdk-go
ADD layer.tar /

EXPOSE 8687

#
# Run wdog daemon inside
CMD [ "/usr/bin/swy-wdog" ]
