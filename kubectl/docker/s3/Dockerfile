FROM golang:latest

WORKDIR /
COPY swy-s3 /usr/bin
RUN chmod +x /usr/bin/swy-s3
RUN mkdir /root/.swysecrets
RUN chmod 0700 /root/.swysecrets

CMD [ "/usr/bin/swy-s3" ]

# Run like this:
# docker run --rm -d --net=host --name=swys3 -v /etc/swifty:/etc/swifty -v /root/.swysecrets:/root/.swysecrets swifty/s3
