FROM swift:latest

WORKDIR /home

COPY swy-wdog /usr/bin/
RUN chmod +x /usr/bin/swy-wdog
COPY runner.swift /swift/runner/Sources/main.swift
COPY Package.swift /swift/runner/

EXPOSE 8687

#
# Prepare middleware
#RUN apt-get update
#RUN apt-get install -y libmysqlclient-dev

#
# Run wdog daemon inside
CMD [ "/usr/bin/swy-wdog" ]
