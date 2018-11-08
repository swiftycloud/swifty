FROM golang:latest

WORKDIR /
COPY swydbscr /usr/bin
RUN chmod +x /usr/bin/swydbscr

CMD [ "/usr/bin/swydbscr", "-conf", "/etc/swifty/conf/scraper.yaml" ]

# Run like this:
# docker run --rm -d --net=host --name=swydbscr -v /etc/swifty:/etc/swifty swifty/swydbscr
