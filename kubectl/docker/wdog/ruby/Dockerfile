FROM ruby:latest

WORKDIR /home/swifty
RUN gem install mongo
COPY swy-wdog /usr/bin/
COPY swy-runner /usr/bin/
RUN chmod +x /usr/bin/swy-wdog
COPY runner.rb /home/swifty
RUN chmod +x /home/swifty/runner.rb

EXPOSE 8687

#
# Run wdog daemon inside
CMD [ "/usr/bin/swy-wdog" ]
