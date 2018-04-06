FROM python:3

WORKDIR /home

RUN pip install pymysql pymongo boto3
COPY swy-wdog /usr/bin/
COPY swifty.py /swifty/
COPY swy-runner.py /usr/bin/
RUN chmod +x /usr/bin/swy-wdog
RUN chmod +x /usr/bin/swy-runner.py
EXPOSE 8687
ENV PYTHONPATH=/swifty

#
# Run wdog daemon inside
CMD [ "/usr/bin/swy-wdog" ]
