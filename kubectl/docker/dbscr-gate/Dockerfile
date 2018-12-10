FROM golang:latest

WORKDIR /
COPY swy-dbscr /usr/bin
RUN chmod +x /usr/bin/swy-dbscr

CMD [ "/usr/bin/swy-dbscr", "-conf", "/etc/swifty/conf/scraper.yaml" ]

# Run like this:
# docker run --rm -d --net=host --name=swydbscr -v /etc/swifty:/etc/swifty swifty/swydbscr
