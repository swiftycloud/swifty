FROM golang:latest

WORKDIR /
RUN apt-get update
COPY swy-wdog /usr/bin/
ENV SWD_INSTANCE=proxy
ENV SWD_CRESPONDER=/var/run/swifty
ENV SWD_PORT=8687
EXPOSE 8687

#
# Run wdog daemon inside
CMD [ "/usr/bin/swy-wdog" ]
