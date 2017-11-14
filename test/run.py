#!/usr/bin/env python3

import http.client
import json
import sys
import os
import subprocess
import tempfile
import distutils.dir_util
import time
import yaml
import argparse
import random
import string
import enum

swifty_default_config="../src/conf/swy-gate.yaml"

class mware:
	def __init__(self):
		self.mw = []

	def add(self, typ, name):
		self.mw.append({'id': name, 'type': typ})

	def desc(self, full = False):
		if full:
			return self.mw
		else:
			return [ { 'id': x['id'] } for x in self.mw ]

	def list(self):
		return [ x['id'] for x in self.mw ]

class function:
	def __init__(self, name, lang, fname = None):
		try:
			os.mkdir('.repo')
		except:
			pass

		if not fname:
			fname = name

		#BUG: No -, . or _ in names de-facto
		self.name = fname

		self._repo = tempfile.mkdtemp(dir = '.repo')
		subprocess.check_call(["git", "-C", self._repo, "init-db", "-q"])
		distutils.dir_util.copy_tree('functions/' + lang['n'] + '/' + name, self._repo)
		subprocess.check_call(["git", "-C", self._repo, "add", '.'])
		subprocess.check_call(["git", "-C", self._repo, "commit",
			"-a", "-q", "-m", "sources", "--author='A <a@foo.org>'"],
			env = {'GIT_COMMITTER_NAME': 'C',
				'GIT_COMMITTER_EMAIL': '<c@foo.org>'})

		if os.access(self._repo + "/install.sh", os.X_OK):
			p = subprocess.Popen(["./install.sh"], cwd = self._repo).wait()
			subprocess.check_call(["git", "-C", self._repo, "add", '.'])
			subprocess.check_call(["git", "-C", self._repo, "commit",
				"-a", "-q", "-m", "deps", "--author='A <a@foo.org>'"],
				env = {'GIT_COMMITTER_NAME': 'C',
					'GIT_COMMITTER_EMAIL': '<c@foo.org>'})

		self.desc = {
			'name': self.name,
			'repo': os.path.abspath(self._repo),
			'script': {
				'lang': lang['n'],
			},
		}

		if lang.get('run'):
			self.desc['script']['run'] = lang['run']

	def update(self):
		# Ugh :(
		if lang['n'] == 'python':
			fname = 'run.py'
		elif lang['n'] == 'golang':
			fname = 'run.go'
		elif lang['n'] == 'swift':
			fname = 'Sources/main.swift'
		else:
			return
		subprocess.check_call(["sed", "-i", self._repo + '/' + fname, "-e", "s/:arg:/:arg2:/"])
		subprocess.check_call(["git", "-C", self._repo, "commit",
			"-a", "-q", "-m", "update", "--author='A <a@foo.org>'"],
			env = {'GIT_COMMITTER_NAME': 'C',
				'GIT_COMMITTER_EMAIL': '<c@foo.org>'})

	def cleanup(self):
		print("Remove %s" % self._repo)
		distutils.dir_util.remove_tree(self._repo)
		self._repo = None

	def setenv(self, envl):
		self.desc['script']['env'] = envl

	def setmware(self, mwarel):
		self.desc['mware'] = mwarel.desc()

	def setevent(self, evt):
		self.desc['event'] = evt

class Api(enum.Enum):
	FUNCTION_ADD		= "/v1/function/add"
	FUNCTION_UPDATE		= "/v1/function/update"
	FUNCTION_REMOVE		= "/v1/function/remove"
	FUNCTION_RUN		= "/v1/function/run"
	FUNCTION_LIST		= "/v1/function/list"
	FUNCTION_LOGS		= "/v1/function/logs"
	MWARE_ADD		= "/v1/mware/add"
	MWARE_GET		= "/v1/mware/get"
	MWARE_REMOVE		= "/v1/mware/remove"

class client:
	def __init__(self, config):
		self._addr = config['daemon']['address']
		self._project = opts.project
		self._local_git = config['daemon']['sources']['clone']
		self._shared_co = config['daemon']['sources']['share'].split(':')[0]
		self._auth_token = None

	def login(self, user, pswd):
		conn = http.client.HTTPConnection(self._addr)
		body = { 'username': user, 'password': pswd }
		conn.request('POST', '/v1/user/login', json.dumps(body))
		resp = conn.getresponse()
		self._auth_token = resp.getheader("X-Subject-Token")
		print('Got auth %s token' % self._auth_token[:18])

	def _req(self, req, body):
		conn = http.client.HTTPConnection(self._addr)
		body['project'] = self._project
		hdrs = { 'X-Subject-Token': self._auth_token, 'Content-Type': 'application/json' }
		# print('B[%s]' % json.dumps(body))
		conn.request('POST', req, json.dumps(body), hdrs)
		return conn.getresponse()

	def _req_api(self, req_api, body):
		return self._req(req_api.value, body)

	def _resp(self, resp):
		x = resp.read()
		return json.loads(x.decode())

	def fn_list(self):
		print('Listing functions:')
		resp = self._req_api(Api.FUNCTION_LIST, {})
		if resp.status == 200:
			print(resp.read())

	def fn_add(self, fn):
		print('Adding function %s' % fn.name)
		resp = self._req_api(Api.FUNCTION_ADD, fn.desc)
		if resp.status != 200:
			raise Exception("Adding function %s failed (%s)" % (fn.name, resp.reason))

	def mw_add(self, mwares):
		print('Adding mwares')
		resp = self._req_api(Api.MWARE_ADD, {'mware': mwares.desc(True)})
		if resp.status != 200:
			raise Exception("Adding mware failed (%s)" % resp.reason)

	def fn_update(self, fn):
		print('Updating function %s' % fn.name)
		resp = self._req_api(Api.FUNCTION_UPDATE, { 'name': fn.name })
		if resp.status != 200:
			raise Exception("Updating function failed (%s)" % resp.reason)

	def run(self, fn, args):
		print('Running function %s' % fn.name)
		resp = self._req_api(Api.FUNCTION_RUN, { 'name': fn.name, 'args': args })
		if resp.status != 200:
			print("Error running %s: %s" % (fn.name, resp.reason))
			return None

		resp = self._resp(resp)
		if resp:
			print("`-OUT:[%s]" % resp['stdout'])
			if resp['stderr']:
				print("`-ERR:[%s]" % resp['stderr'])
		return resp

	def fn_remove(self, fn):
		print('Removing function %s' % fn.name)
		resp = self._req_api(Api.FUNCTION_REMOVE, { 'name' : fn.name })
		if resp.status != 200:
			raise Exception("Removing function %s failed (%s)" % (fn.name, resp.reason))
		fn.cleanup()

	def fn_logs(self, fn):
		resp = self._req_api(Api.FUNCTION_LOGS, { 'name': fn.name })
		if resp.status != 200:
			return None
		return self._resp(resp)

	def mw_remove(self, mwares):
		print('Removing mwares')
		resp = self._req_api(Api.MWARE_REMOVE, {'mware': mwares.list()})
		if resp.status != 200:
			raise Exception("Removing mware failed (%s)" % resp.reason)

	def waitall(self):
		wait = ['all FNs']
		wtime = 0.5
		while wait:
			print("Wait for %s to show up" % ", ".join(wait[:3]))

			time.sleep(wtime)
			if wtime < 5.0:
				wtime *= 2.0

			resp = self._req_api(Api.FUNCTION_LIST, {})
			if resp.status != 200:
				raise Exception("Cannot list functions")

			funcs = self._resp(resp)
			wait = []
			for fn in funcs:
				if not fn['state'] in ('ready', 'stalled'):
					wait.append(fn['name'])

		print("All FNs are ready")


def randstr():
	return ''.join(random.choice(string.ascii_letters) for _ in range(0,8))


class test_hw:
	def __init__(self, lang, clnt):
		self.fn = function('hw', lang)
		clnt.fn_add(self.fn)

	def run(self, client):
		cookie1 = randstr()
		cookie2 = randstr()
		res = client.run(self.fn, [ cookie1, cookie2 ])
		if not res or res['stderr']:
			return False
		return res['stdout'] == '%s:arg:%s.%s\n' % (lang['n'], cookie1, cookie2)

	def cleanup(self, clnt):
		clnt.fn_remove(self.fn)


class test_env:
	def __init__(self, lang, clnt):
		self.cookie = randstr()
		self.fn = function('env', lang)
		self.fn.setenv([ 'FAAS_FOO=%s' % self.cookie ])
		clnt.fn_add(self.fn)

	def run(self, client):
		res = client.run(self.fn, [])
		if not res or res['stderr']:
			return False
		return res['stdout'] == '%s:env:%s\n' % (lang['n'], self.cookie)

	def cleanup(self, clnt):
		clnt.fn_remove(self.fn)

class test_sql:
	def __init__(self, lang, clnt):
		self.mwn = 'testsql'
		self.mw = mware()
		self.mw.add('sql', self.mwn)
		self.fn = function('sql', lang)
		self.fn.setmware(self.mw)

		clnt.mw_add(self.mw)
		clnt.fn_add(self.fn)

	def run(self, client):
		cookie = randstr()
		res = client.run(self.fn, ['create', self.mwn ])
		if not res or res['stderr']:
			return False

		res = client.run(self.fn, ['insert', self.mwn, cookie])
		if not res or res['stderr']:
			return False

		res = client.run(self.fn, ['select', self.mwn ])
		if not res or res['stderr']:
			return False

		return res['stdout'] == '%s:sql:%s\n' % (lang['n'], cookie)

	def cleanup(self, clnt):
		clnt.fn_remove(self.fn)
		clnt.mw_remove(self.mw)


class test_rmq:
	def __init__(self, lang, clnt):
		self.mw = mware()
		self.mw.add('mq', 'testrabbit')
		self.fn = function('rmq', lang)
		self.fn.setmware(self.mw)

		clnt.mw_add(self.mw)
		clnt.fn_add(self.fn)

	def run(self, client):
		cookie = randstr()
		res = client.run(self.fn, ['send', "tq", cookie])
		if not res or res['stderr']:
			return False

		res = client.run(self.fn, ['recv', "tq"])
		if not res or res['stderr']:
			return False

		return res['stdout'] == '%s:mq:%s\n' % (lang['n'], cookie)

	def cleanup(self, clnt):
		clnt.fn_remove(self.fn)
		clnt.mw_remove(self.mw)


class test_ones:
	def __init__(self, lang, clnt):
		self.fn = function('hw', lang, fname = 'oneshw')
		self.fn.setevent({'source': 'oneshot'})
		clnt.fn_add(self.fn)

	def run(self, clnt):
		wtime = 0.5
		while wtime > 0.0:
			print("`- Waiting for log entry")
			time.sleep(wtime)
			resp = clnt.fn_logs(self.fn)

			for l in resp or []:
				try:
					lt = time.mktime(time.strptime(l['ts'][:22].strip(), "%Y-%m-%d %H:%M:%S.%f"))
					if lt < now:
						continue
				except:
					print("Time [%s] conversion failed" % l['ts'])

				if l['stdout'] == '%s:arg:\n' % (lang['n']):
					return True
				print("`- alien message [%s]" % l['stdout'])

			if wtime < 2.0:
				wtime *= 2.0
			else:
				print("The evtr routine seems to be not triggered")
				break

		return False

	def cleanup(self, clnt):
		clnt.fn_remove(self.fn)

class test_evt:
	def __init__(self, lang, clnt):
		self.mw = mware()
		self.mw.add('mq', 'testrabbit')
		self.fns = function('rmq', lang, fname = 'evts')
		self.fns.setmware(self.mw)
		self.fnr = function('hw', lang, fname = 'evtr')
		self.fnr.setmware(self.mw)
		self.fnr.setevent({'source': 'mware', 'mwid': 'testrabbit', 'mqueue': 'eq'})

		# BUG: failure to add doesn't clean the rest
		clnt.mw_add(self.mw)
		clnt.fn_add(self.fns)
		clnt.fn_add(self.fnr)

	def run(self, clnt):
		cookie = randstr()
		now = time.mktime(time.localtime())
		res = clnt.run(self.fns, [ 'send', 'eq', cookie ])
		if not res or res['stderr']:
			return False

		# BUG: how to wait for more logs?
		# BUG: how to reset logs?
		wtime = 0.5
		while wtime > 0.0:
			print("`- Waiting for log entry")
			time.sleep(wtime)
			resp = clnt.fn_logs(self.fnr)

			for l in resp or []:
				try:
					lt = time.mktime(time.strptime(l['ts'][:22].strip(), "%Y-%m-%d %H:%M:%S.%f"))
					if lt < now:
						continue
				except:
					print("Time [%s] conversion failed" % l['ts'])

				if l['text'] == 'out: [%s:arg:%s\n], err: []' % (lang['n'], cookie):
					return True
				print("`- alien message [%s]" % l['text'])

			if wtime < 2.0:
				wtime *= 2.0
			else:
				print("The evtr routine seems to be not triggered")
				break

		return False

	def cleanup(self, clnt):
		clnt.fn_remove(self.fnr)
		clnt.fn_remove(self.fns)
		clnt.mw_remove(self.mw)
		print("All stuff left as is")


class test_shmw:
	def __init__(self, lang, clnt):
		self.mwn = 'shsql'
		self.mw = mware()
		self.mw.add('sql', self.mwn)
		self.fna = function('sql', lang, fname = 'ssqla')
		self.fna.setmware(self.mw)
		self.fnb = function('sql', lang, fname = 'ssqlb')
		self.fnb.setmware(self.mw)

		clnt.mw_add(self.mw)
		clnt.fn_add(self.fna)
		clnt.fn_add(self.fnb)

	def run(self, client):
		cookie = randstr()
		res = client.run(self.fna, ['create', self.mwn ])
		if not res or res['stderr']:
			return False

		res = client.run(self.fna, ['insert', self.mwn, cookie])
		if not res or res['stderr']:
			return False

		# Also checks that mware is still alive
		client.fn_remove(self.fna)

		res = client.run(self.fnb, ['select', self.mwn ])
		if not res or res['stderr']:
			return False

		return res['stdout'] == '%s:sql:%s\n' % (lang['n'], cookie)

	def cleanup(self, clnt):
		# The fna was removed while running
		clnt.fn_remove(self.fnb)
		clnt.mw_remove(self.mw)


class test_2mw:
	def __init__(self, lang, clnt):
		self.mwn1 = 'msql1'
		self.mwn2 = 'msql2'
		self.mw = mware()
		self.mw.add('sql', self.mwn1)
		self.mw.add('sql', self.mwn2)
		self.fn = function('sql', lang, fname = 'msql')
		self.fn.setmware(self.mw)

		clnt.mw_add(self.mw)
		clnt.fn_add(self.fn)

	def run(self, client):
		cookie1 = randstr()
		cookie2 = randstr()

		res = client.run(self.fn, ['create', self.mwn1 ])
		if not res or res['stderr']:
			return False

		res = client.run(self.fn, ['create', self.mwn2 ])
		if not res or res['stderr']:
			return False

		res = client.run(self.fn, ['insert', self.mwn1, cookie1])
		if not res or res['stderr']:
			return False

		res = client.run(self.fn, ['insert', self.mwn2, cookie2])
		if not res or res['stderr']:
			return False

		res = client.run(self.fn, ['select', self.mwn1 ])
		if not res or res['stderr']:
			return False
		if res['stdout'] != '%s:sql:%s\n' % (lang['n'], cookie1):
			return False

		res = client.run(self.fn, ['select', self.mwn2 ])
		if not res or res['stderr']:
			return False
		if res['stdout'] != '%s:sql:%s\n' % (lang['n'], cookie2):
			return False

		return True

	def cleanup(self, clnt):
		clnt.fn_remove(self.fn)
		clnt.mw_remove(self.mw)

class test_upd:
	def __init__(self, lang, clnt):
		self.fn = function('hw', lang, fname = 'hwupd')
		clnt.fn_add(self.fn)

	def run(self, client):
		cookie = randstr()
		res = client.run(self.fn, ['v1', cookie ])
		if not res or res['stderr']:
			return False
		if res['stdout'] != '%s:arg:v1.%s\n' % (lang['n'], cookie):
			return False

		self.fn.update()
		client.fn_update(self.fn)

		# BUG: how to wait for update to finish?
		wtime = 0.5
		while wtime > 0.0:
			print("`- Waiting for fn to update")
			time.sleep(wtime)
			res = client.run(self.fn, [cookie, 'v2' ])
			if res and not res['stderr']:
				if res['stdout'] == '%s:arg2:%s.v2\n' % (lang['n'], cookie):
					break

				if res['stdout'] != '%s:arg:%s.v2\n' % (lang['n'], cookie):
					print("Function corrupted while updating")
					return False

			# BUG: Still no rolling update, fn is removed,
			# then inserted back :( so on error we just wait
			# more
			if wtime >= 16.0:
				print("The function isn't updated for too long")
				return False

			wtime *= 2.0
			continue

		return True

	def cleanup(self, clnt):
		clnt.fn_remove(self.fn)


languages = [
	{	'n': 'python',
		'run': 'run.py'
	},
	{	'n': 'golang',
	},
	{	'n': 'swift',
		'run': 'run',
	},
]

flavors = [
	# Checks arguments propagaion
	{ 'id': 'hw',	'class': test_hw },
	# Checks add-time environment is set up
	{ 'id': 'env',	'class': test_env },
	# Checks SQL middleware (single function)
	{ 'id': 'sql',	'class': test_sql },
	# Checks RabbitMQ middleware (single function)
	{ 'id': 'mq',	'class': test_rmq },
	# Checks Rabbit event works
	{ 'id': 'evt',	'class': test_evt },
	# Checks shared mware (sql)
	{ 'id': 'shsql', 'class': test_shmw },
	# Checks two mware per FN (sql)
	{ 'id': '2sql',	'class': test_2mw },
	# Checks update facility
	{ 'id': 'upd',  'class': test_upd },
	# Checks one shot run
	{ 'id': 'ones', 'class': test_ones },
]

def lid(lang):
	return lang.get('id', lang['n'])

argp = argparse.ArgumentParser("Switfy test suite")
argp.add_argument("-l", "--lang", help="Language to test", choices = [ lid(x) for x in languages ])
argp.add_argument("-f", "--flav", help="Test flavor to run", choices = [ x['id'] for x in flavors ])
argp.add_argument("-K", "--keep", help="Keep container and mware after run", action='store_true')
argp.add_argument("-c", "--conf", help="Configuration file", dest = 'conf', default = swifty_default_config)
argp.add_argument("-p", "--project", help="A project name to use", dest = 'project', default = 'swytest')
argp.add_argument("-u", "--user", help="A user name to login", dest = 'login_user', default = "xemul.user")
argp.add_argument("-s", "--pass", help="A password to login", dest = 'login_pass', default = "123456")
opts = argp.parse_args()

cfg = yaml.load(open(opts.conf))
c = client(cfg)
c.login(opts.login_user, opts.login_pass)

class fake_test:
	def __init__(self):
		pass

	def run(self, clnt):
		return False

	def cleanup(self, clnt):
		pass


for lang in languages:
	lang['tpass'] = []
	lang['tfail'] = []
	if opts.lang and lid(lang) != opts.lang:
		continue

	print("*"*60)
	print((" Running tests for %s " % lid(lang)).center(60, "*"))
	print("*"*60)

	tests = []
	for fl in flavors:
		if opts.flav and fl['id'] != opts.flav:
			continue
		print((" Adding %s " % fl['id']).center(60, "."))
		try:
			t = fl['class'](lang, c)
		except:
			print((" %s FAIL to start " % fl['id']).center(60, '-'))
			lang['tfail'] += [ fl ]
		else:
			t.flavor = fl
			tests.append(t)

	if tests:
		c.waitall()

	print(" Running tests ".center(60, "="))
	for t in tests:
		print((" Running %s " % t.flavor['id']).center(60, "-"))
		if t.run(c):
			t.result = True
			print((" %s PASS " % t.flavor['id']).center(60, "."))
		else:
			t.result = False
			print((" %s FAIL " % t.flavor['id']).center(60, "."))

	if not opts.keep:
		print(" Cleaning ".center(60, "~"))
		for t in tests:
			try:
				t.cleanup(c)
			except:
				t.result = False
				print((" %s FAIL while cleaning up " % t.flavor['id']).center(60, '-'))
	else:
		print(" Kept containers and mware ".center(60, "~"))

	lang['tpass'] += [ t.flavor for t in tests if t.result ]
	lang['tfail'] += [ t.flavor for t in tests if not t.result ]


print("*"*60)
print(" Summary ".center(60, "*"))
print("*"*60)
print(" "*10 + "".join(["%10s" % lid(l) for l in languages]))
for f in flavors:
	def tres(f, l):
		if f in l['tpass']:
			return 'PASS'
		if f in l['tfail']:
			return 'FAIL'
		return 'skip'
	print("%9s:" % f['id'] + "".join([ "%10s" % tres(f, l) for l in languages ]))
