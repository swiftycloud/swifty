FROM golang:latest

WORKDIR /
COPY swy-admd /usr/bin
RUN chmod +x /usr/bin/swy-admd
RUN mkdir /root/.swysecrets
RUN chmod 0700 /root/.swysecrets

CMD [ "/usr/bin/swy-admd" ]

# Run like this:
# docker run --rm -d --net=host --name=swyadmd -v /etc/swifty:/etc/swifty -v /root/.swysecrets:/root/.swysecrets -v /etc/letsencrypt:/etc/letsencrypt swifty/admd
