import pymysql.cursors
import os
import sys

mwn = sys.argv[2].upper()
dbaddr = os.getenv('MWARE_' + mwn + '_ADDR').split(':')[0]
dbuser = os.getenv('MWARE_' + mwn + '_USER')
dbpass = os.getenv('MWARE_' + mwn + '_PASS')
dbname = os.getenv('MWARE_' + mwn + '_DBNAME')

connection = pymysql.connect(host=dbaddr,
		user=dbuser, password=dbpass, db=dbname,
		cursorclass=pymysql.cursors.DictCursor)

try:
	with connection.cursor() as cursor:
		if sys.argv[1] == 'create':
			sql = "CREATE TABLE `foo` (`id` int(11) NOT NULL AUTO_INCREMENT, " \
				    "`name` varchar(255), PRIMARY KEY(`id`));"
			cursor.execute(sql)
		elif sys.argv[1] == 'insert':
			sql = "INSERT INTO `foo` (`name`) VALUES (%s);"
			cursor.execute(sql, (sys.argv[3],))
		elif sys.argv[1] == 'select':
			sql = "SELECT * FROM `foo`;"
			cursor.execute(sql)
			result = cursor.fetchone()
			print('python:sql:' + result['name'])
	connection.commit()
finally:
	connection.close()
