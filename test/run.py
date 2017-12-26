#!/usr/bin/env python3

import subprocess
import http.client
import json
import time
import random
import string
import argparse

def randstr():
	return ''.join(random.choice(string.ascii_letters) for _ in range(0,8))

lext = { 'python' : '.py', 'golang' : '.go' }
swyctl = "./swyctl"

def swyrun(cmdl):
	print(" ".join(cmdl))
	subprocess.check_call([ swyctl ] + cmdl)

def swyrun2(cmdl):
	print(" ".join(cmdl))
	cmd = subprocess.Popen([ swyctl ] + cmdl, stdout = subprocess.PIPE)
	ret = cmd.stdout.readlines()
	cmd.wait()
	return [ i.decode('utf-8') for i in ret ]

def list_fn():
	fns = swyrun2([ 'ls' ])
	return [ i.split()[0].strip() for i in fns[1:] ]

def add_fn(name, lang, mw = [], evt = "url", tmo = None):
	cmd = [ "add", name, "-lang", lang,
		"-src", "test/functions/" + lang + "/" + name + lext[lang],
		"-event", evt ]
	if mw:
		cmd += [ "-mw", ",".join(mw) ]
	if tmo:
		cmd += [ "-tmo", "%d" % tmo ]
	swyrun(cmd)

	return _wait_fn(name)

def upd_fn(inf, lang):
	swyrun([ "upd", inf['name'], "-src", "test/functions/" + lang + "/" + name + "2" + lext[lang] ])

def add_mw(typ, name):
	swyrun([ "madd", name, typ ])

def del_mw(name):
	swyrun([ "mdel", name ])

def _get_inf_fn(name):
	inf = swyrun2([ "inf", name ])
	ret = { i[0].strip(): i[1].strip() for i in [ i.split(':', 1) for i in inf ] }
	ret["name"] = name
	return ret

def inf_fn(inf):
	return _get_inf_fn(inf['name'])

def _wait_fn(name):
	tmo = 1
	while True:
		time.sleep(tmo)
		inf = _get_inf_fn(name)
		if inf['State'] == 'ready':
			return inf
		tmo *= 2

def run_fn(inf, args):
	url = inf['URL'].split('/', 3)
	conn = http.client.HTTPConnection(url[2])
	conn.request('GET', '/' + url[3] + '?' + '&'.join([x[0]+'='+x[1] for x in args.items()]))
	resp = conn.getresponse()
	if resp.status != 200:
		return "ERROR:%d" % resp.status

	rv = resp.read()
	return json.loads(rv)

def del_fn(inf):
	swyrun([ "logs", inf['name'] ])
	swyrun([ "del", inf['name'] ])


def run_test(fname, langs, opts):
	print("====== Running %s" % fname.__name__)
	for l in langs:
		print("______ %s" % l)
		if fname(l, opts):
			print("------ PASS")
		else:
			print("====== FAIL")

def helloworld(lang, opts):
	cookie = randstr()
	inf = add_fn("helloworld", lang)
	ret = run_fn(inf, {'name': cookie})
	print(ret)
	if not opts['keep']:
		del_fn(inf)
	return ret['message'] == 'hw:%s:%s' % (lang, cookie)

def update(lang, opts):
	ok = False
	cookie = randstr()
	inf = add_fn("helloworld", lang)
	ret = run_fn(inf, {'name': cookie})
	print(ret)
	if ret['message'] == 'hw:%s:%s' % (lang, cookie):
		upd_fn(inf, lang)
		tmo = 0.5
		while tmo < 12.0:
			ret = run_fn(inf, {'name': cookie})
			print(ret)
			if ret['message'] == 'hw:%s:%s' % (lang, cookie):
				print("Updating")
				time.sleep(tmo)
				tmo *= 2.0
				continue
			if ret['message'] == 'hw2:%s:%s' % (lang, cookie):
				ok = True
				print("Updated")
			else:
				print("Alien message")
			break
	if not opts['keep']:
		del_fn(inf)
	return ok

def maria(lang, opts):
	ok = False
	dbname = 'mysqltst'
	cookie = randstr()
	args_c = { 'dbname': dbname, 'action': 'create' }
	args_i = { 'dbname': dbname, 'action': 'insert', 'key': 'foo', 'val': cookie }
	args_s = { 'dbname': dbname, 'action': 'select', 'key': 'foo' }


	add_mw("maria", dbname)
	inf = add_fn("maria", lang, mw = [ dbname ])
	ret = run_fn(inf, args_c)
	print(ret)
	if ret.get('res', '') == 'done':
		ret = run_fn(inf, args_i)
		print(ret)
		if ret.get('res', '') == 'done':
			ret = run_fn(inf, args_s)
			print(ret)
			if ret.get('res', '').strip() == cookie:
				ok = True
	if not opts['keep']:
		del_fn(inf)
		del_mw(dbname)
	return ok

def pgsql(lang, opts):
	ok = False
	dbname = 'pgtst'
	cookie = randstr()
	args_c = { 'dbname': dbname, 'action': 'create' }
	args_i = { 'dbname': dbname, 'action': 'insert', 'key': 'foo', 'val': cookie }
	args_s = { 'dbname': dbname, 'action': 'select', 'key': 'foo' }


	add_mw("postgres", dbname)
	inf = add_fn("pgsql", lang, mw = [ dbname ])
	ret = run_fn(inf, args_c)
	print(ret)
	if ret.get('res', '') == 'done':
		ret = run_fn(inf, args_i)
		print(ret)
		if ret.get('res', '') == 'done':
			ret = run_fn(inf, args_s)
			print(ret)
			if ret.get('res', [['']])[0][0].strip() == cookie:
				ok = True
	if not opts['keep']:
		del_fn(inf)
		del_mw(dbname)
	return ok

def mongo(lang, opts):
	ok = False
	dbname = 'mgotst'
	cookie = randstr()
	args_i = { 'dbname': dbname, 'collection': 'tcol', 'action': 'insert', 'key': 'foo', 'val': cookie }
	args_s = { 'dbname': dbname, 'collection': 'tcol', 'action': 'select', 'key': 'foo' }

	add_mw("mongo", dbname)
	inf = add_fn("mongo", lang, mw = [ dbname ])
	ret = run_fn(inf, args_i)
	print(ret)
	if ret.get('res', '') == 'done':
		ret = run_fn(inf, args_s)
		print(ret)
		if ret.get('res', '') == cookie:
			ok = True
	if not opts['keep']:
		del_fn(inf)
		del_mw(dbname)
	return ok

def s3(lang, opts):
	ok = False
	s3name = 'tbuck'
	cookie = randstr()
	args_p = { 'action': 'put',  'bucket': s3name , 'name': 'tobj', 'data': cookie }
	args_g = { 'action': 'get',  'bucket': s3name , 'name': 'tobj' }

	add_mw("s3", s3name)
	inf = add_fn("s3", lang, mw = [ s3name ])
	ret = run_fn(inf, args_p)
	print(ret)
	if ret.get('res', '') == 'done':
		ret = run_fn(inf, args_g)
		print(ret)
		if ret.get('res', '') == cookie:
			ok = True
	if not opts['keep']:
		del_fn(inf)
		del_mw(s3name)
	return ok

def s3notify(lang, opts):
	ok = False
	s3name = 's3nns'
	args_c = { 's3name': s3name, 'action': 'create', 'bucket': 'tbuck' }
	args_p = { 's3name': s3name, 'action': 'put',    'bucket': 'tbuck' , 'name': 'xobj', 'data': 'abc' }

	add_mw("s3", s3name)
	inf = add_fn("s3", lang, mw = [ s3name ])
	ret = run_fn(inf, args_c)
	print(ret)

	if ret.get('res', '') == 'done':
		infe = add_fn("s3evt", lang, evt = 'mware:%s:b=tbuck' % s3name)
		ret = run_fn(inf, args_p)
		print(ret)
		if ret.get('res', '') == 'done':
			infe = inf_fn(infe)
			if infe['Called'] == '0':
				ok = True
		if not opts['keep']:
			del_fn(infe)

	if not opts['keep']:
		del_fn(inf)
		del_mw(s3name)
	return ok

def timeout(lang, opts):
	ok = False
	inf = add_fn("timeout", lang, tmo = 2000)
	ret = run_fn(inf, { 'tmo': '1000' })
	print(ret)
	if ret == 'slept:1000':
		ret = run_fn(inf, { 'tmo': '3000' })
		print(ret)
		if ret == 'ERROR:524': # Timeout
			ret = run_fn(inf, { 'tmo': '1500' })
			print(ret)
			if ret == 'slept:1500':
				ok = True
	if not opts['keep']:
		del_fn(inf)
	return ok

def checkempty(lang, opts):
	fns = list_fn()
	print(fns)
	return len(fns) == 0

tests = [
	(helloworld,	["python", "golang"]),
	(update,	["python"]),
	(pgsql,		["python"]),
	(maria,		["python"]),
	(mongo,		["python"]),
	(s3,		["python"]),
	(s3notify,	["python"]),
	(timeout,	["python"]),
	(checkempty,	[""]),
]

def list_tests(opts):
	print("Tests:")
	for t in tests:
		print("\t%s" % t[0].__name__)

def run_tests(opts):
	tn = opts.get('test', '')
	for t in tests:
		if tn and t[0].__name__ != tn:
			continue
		run_test(t[0], t[1], opts)


p = argparse.ArgumentParser("Swifty tests")
sp = p.add_subparsers(help = "Use --help for list of actions")

lp = sp.add_parser("ls", help = "List tests")
lp.set_defaults(action = list_tests)
rp = sp.add_parser("run", help = "Run tests")
rp.add_argument("-t", "--test", help = "Name of test")
rp.add_argument("-k", "--keep", help = "Keep fns and mware", action = 'store_true')
rp.set_defaults(action = run_tests)

def show_help(opts):
	print("Run with --help for list of options")

opts = vars(p.parse_args())
opts.get('action', show_help)(opts)
