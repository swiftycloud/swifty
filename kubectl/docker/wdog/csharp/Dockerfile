FROM mono:latest

WORKDIR /home/swifty
COPY swy-wdog /usr/bin/
COPY swy-runner /usr/bin/
RUN chmod +x /usr/bin/swy-wdog
COPY runner.cs /mono/runner/
COPY XStream.dll /mono/runner/

EXPOSE 8687

#
# Run wdog daemon inside
CMD [ "/usr/bin/swy-wdog" ]
