FROM golang:latest

WORKDIR /
COPY swy-dbscr-s3 /usr/bin
RUN chmod +x /usr/bin/swy-dbscr-s3

CMD [ "/usr/bin/swy-dbscr-s3", "-conf", "/etc/swifty/conf/scraper-s3.yaml" ]

# Run like this:
# docker run --rm -d --net=host --name=swydbscr -v /etc/swifty:/etc/swifty swifty/swydbscr
