FROM python:3

WORKDIR /home

RUN pip install py-postgresql pymongo boto3
COPY swy-wdog /usr/bin/
RUN chmod +x /usr/bin/swy-wdog
EXPOSE 8687

#
# Run wdog daemon inside
CMD [ "/usr/bin/swy-wdog" ]
