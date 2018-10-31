import sys
import os
import subprocess

ten = sys.args[1]
ppath = "/packages/" + ten + "/python"
sys.path += [ ppath + z for z in sys.path ]
from pkg_resources import working_set, get_distribution

local_set = { p.project_name: p for p in working_set if p.location.startswith(ppath) }

if sys.argv[2] == "list":
    for p in local_set:
        print(p)

if sys.argv[2] == "remove":
    pkg = local_set.get(sys.argv[3], None)
    if pkg == None:
        sys.exit(1)

    env = os.environ.copy()
    env["PYTHONPATH"] = pkg.location
    subprocess.check_call(["pip", "uninstall", "-y", pkg.project_name], env=env)
