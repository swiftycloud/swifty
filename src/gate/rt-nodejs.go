package main

var nodejs_info = langInfo {
	Ext:		"js",
	CodePath:	"/function",

	/*
	 * Install -- use npm install
	 * List    -- list top-level dirs with package.json inside
	 * Remove  -- manualy remove the dir
	 */
	RunPkgPath:	nodeModules,
}

func nodeModules(id SwoId) (string, string) {
	/*
	 * Node's runner-js.sh sets /home/packages/node_modules as NODE_PATH
	 */
	return packagesDir() + "/" + id.Tennant + "/nodejs", "/home/packages"
}
