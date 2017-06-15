FROM swift:latest

WORKDIR /home

COPY swy-wdog /usr/bin/
RUN chmod +x /usr/bin/swy-wdog

EXPOSE 8687

#
# Prepare middleware
RUN apt-get update
RUN apt-get install -y libmysqlclient-dev

#
# Run wdog daemon inside
CMD [ "/usr/bin/swy-wdog" ]
