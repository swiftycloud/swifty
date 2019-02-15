FROM golang:latest

WORKDIR /
RUN apt-get update
COPY swyctl /usr/bin
ENV SWIFTY_HOME=/tmp
CMD [ "/usr/bin/swyctl" ]

# Run like this:
# docker run -e SWIFTY_LOGIN=user:pass@host:port
#    [ -e SWIFTY_PASSWORD=... if the pass has @-s in ] swifty/client <args>
