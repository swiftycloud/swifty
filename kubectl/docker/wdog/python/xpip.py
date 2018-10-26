import sys
import os
import subprocess

sys.path += [ "/packages" + z for z in sys.path ]
from pkg_resources import working_set, get_distribution

local_set = { p.project_name: p for p in working_set if p.location.startswith("/packages") }

if sys.argv[1] == "list":
    for p in local_set:
        print(p)

if sys.argv[1] == "remove":
    pkg = local_set.get(sys.argv[2], None)
    if pkg == None:
        sys.exit(1)

    env = os.environ.copy()
    env["PYTHONPATH"] = pkg.location
    subprocess.check_call(["pip", "uninstall", "-y", pkg.project_name], env=env)
