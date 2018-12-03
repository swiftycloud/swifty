FROM mono:latest

WORKDIR /home/swifty
ADD layer.tar /
EXPOSE 8687

#
# Run wdog daemon inside
CMD [ "/usr/bin/swy-wdog" ]
