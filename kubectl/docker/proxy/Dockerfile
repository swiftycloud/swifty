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

# Run like this
# docker run  -d --net=host --name=swyproxy -v /var/run/swifty/wdogconn:/var/run/swifty -e SWD_POD_IP=95.216.163.129 swifty/proxy
