FROM python:3

WORKDIR /home

RUN pip install --upgrade pip
RUN pip install pymysql pymongo boto3 requests
COPY swy-wdog /usr/bin/
COPY swy-runner /usr/bin/
COPY swifty.py /swifty/
COPY swy-runner.py /usr/bin/
COPY runner-py.sh /usr/bin/start_runner.sh
COPY xpip.py /usr/bin
RUN chmod +x /usr/bin/swy-wdog /usr/bin/xpip.py /usr/bin/start_runner.sh

EXPOSE 8687
ENV PYTHONPATH=/swifty

#
# Run wdog daemon inside
CMD [ "/usr/bin/swy-wdog" ]
