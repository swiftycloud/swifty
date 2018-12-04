#!/usr/bin/env python

import sys
import yaml
import tarfile

rules = yaml.load(open(sys.argv[1]))
defmode = rules.get('mode', 0640)
root = rules.get('root', '')

def setmode(ti, inf):
	ti.mode = inf.get('mode', defmode)
	return ti

with tarfile.open(sys.argv[2], 'w') as tar:
	for f in rules['files']:
		name = f.keys()[0]
		inf = f[name]
		if not inf:
			inf = {}
		aname = inf.get('to', root + '/' + name)
		if aname.endswith('/'):
			aname += name
		print('+ %r as %r' % (name, aname))
		tar.add(name = name, arcname = aname, filter = lambda ti: setmode(ti, inf))
