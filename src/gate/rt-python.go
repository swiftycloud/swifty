package main

var py_info = langInfo {
	Ext:		"py",
	CodePath:	"/function",

	/*
	 * Install -- call pip install
	 * List    -- use pkg_resources, but narrow down it to the /packages
	 * Remove  -- use pip remove, but pre-check that the package is in /packages
	 */
	RunPkgPath:	pyPackages,
}

func pyPackages(id SwoId) (string, string) {
	/* Python runner adds /packages/* to sys.path for every dir met in there */
	return packagesDir() + "/" + id.Tennant + "/python", "/packages"
}
