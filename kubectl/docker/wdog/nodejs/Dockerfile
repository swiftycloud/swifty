FROM node:latest

WORKDIR /home/swifty
RUN npm install jskernel && \
	npm install libjs && \
	npm install libsys && \
	npm install mongodb
ADD layer.tar /

EXPOSE 8687

#
# Run wdog daemon inside
CMD [ "/usr/bin/swy-wdog" ]
