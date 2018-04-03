FROM golang:latest

WORKDIR /
RUN apt-get update && apt-get install -y librados2
COPY swy-s3 /usr/bin
RUN chmod +x /usr/bin/swy-s3
RUN mkdir /root/.swysecrets
RUN chmod 0700 /root/.swysecrets

CMD [ "/usr/bin/swy-s3", "-no-rados" ]

# Run like this:
# docker run --rm -d --net=host --name=swys3 -v /etc/swifty:/etc/swifty -v /root/.swysecrets:/root/.swysecrets -v /etc/letsencrypt:/etc/letsencrypt swifty/s3
