FROM python:3

WORKDIR /home

#
RUN pip install py-postgresql
#
COPY swy-wdog /usr/bin/
RUN chmod +x /usr/bin/swy-wdog
COPY main.py /function/

EXPOSE 8687

#
# Run wdog daemon inside
CMD [ "/usr/bin/swy-wdog" ]
