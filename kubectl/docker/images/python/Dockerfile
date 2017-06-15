FROM python:latest

WORKDIR /home

#
COPY swy-wdog /usr/bin/
RUN chmod +x /usr/bin/swy-wdog

EXPOSE 8687

#
# Prepare middleware
RUN apt-get update
# MariaDB bindings
RUN pip install PyMySQL
# MongoDB bindings
RUN apt-get install -y python3-bson python3-pymongo
# RabbitMQ bindinngs
RUN apt-get install -y python-pika
RUN pip install python3-pika

#
# Run wdog daemon inside
CMD [ "/usr/bin/swy-wdog" ]
