FROM swift:latest

WORKDIR /home

COPY swy-wdog /usr/bin/
COPY swy-runner /usr/bin/
RUN chmod +x /usr/bin/swy-wdog
COPY runner.swift /swift/runner/Sources/main.swift
COPY Package.swift /swift/runner/
COPY runner-swift.sh /usr/bin/start_runner.sh
COPY builder.sh /usr/bin/build_runner.sh
RUN chmod +x /usr/bin/start_runner.sh /usr/bin/build_runner.sh

EXPOSE 8687

#
# Prepare middleware
#RUN apt-get update
#RUN apt-get install -y libmysqlclient-dev

#
# Run wdog daemon inside
CMD [ "/usr/bin/swy-wdog" ]
