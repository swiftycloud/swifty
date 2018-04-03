FROM golang:latest

WORKDIR /
COPY swy-gate /usr/bin
RUN chmod +x /usr/bin/swy-gate
RUN mkdir /root/.swysecrets
RUN chmod 0700 /root/.swysecrets

CMD [ "/usr/bin/swy-gate" ]

# Run like this:
# docker run --rm -d --net=host --name=swygate -v /etc/swifty:/etc/swifty -v /root/.swysecrets:/root/.swysecrets -v /etc/letsencrypt:/etc/letsencrypt -v /home/swifty-volume:/home/swifty-volume swifty/gate
