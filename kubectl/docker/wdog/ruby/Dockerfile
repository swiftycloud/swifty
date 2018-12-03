FROM ruby:latest

WORKDIR /home/swifty
RUN gem install mongo
ADD layer.tar /

EXPOSE 8687

#
# Run wdog daemon inside
CMD [ "/usr/bin/swy-wdog" ]
