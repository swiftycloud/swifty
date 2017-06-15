import pika
import os
import sys

rmqadd, rmqprt = os.getenv('MWARE_TESTRABBIT_ADDR').split(':', 2)
rmqusr = os.getenv('MWARE_TESTRABBIT_USER')
rmqpwd = os.getenv('MWARE_TESTRABBIT_PASS')
rmqvhst = os.getenv('MWARE_TESTRABBIT_VHOST')

def recv_callback(ch, method, properties, body):
	print("python:mq:%s" % body.decode())
	ch.stop_consuming()

creds = pika.PlainCredentials(rmqusr, rmqpwd)
connection = pika.BlockingConnection(pika.ConnectionParameters(host=rmqadd, port=int(rmqprt), virtual_host=rmqvhst, credentials=creds))
channel = connection.channel()
channel.queue_declare(queue=sys.argv[2])
if sys.argv[1] == 'send':
    properties = pika.BasicProperties(user_id=rmqusr)
    channel.basic_publish(exchange='', routing_key=sys.argv[2], body=sys.argv[3], properties=properties)
    print("SENT %s w/ userid" % sys.argv[3])
elif sys.argv[1] == 'recv':
    channel.basic_consume(recv_callback, queue=sys.argv[2], no_ack=True)
channel.start_consuming()
