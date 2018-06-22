FROM node:latest

WORKDIR /home/swifty
RUN npm install jskernel
RUN npm install libjs
RUN npm install libsys
COPY swy-wdog /usr/bin/
COPY swy-runner /usr/bin/
RUN chmod +x /usr/bin/swy-wdog
COPY runner.js /home/swifty
COPY runner-js.sh /home/swifty
RUN chmod +x /home/swifty/runner-js.sh

EXPOSE 8687

#
# Run wdog daemon inside
CMD [ "/usr/bin/swy-wdog" ]
