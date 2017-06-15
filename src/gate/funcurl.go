package main

func urlSetup(conf *YAMLConf, fn *FunctionDesc, on bool) error {
	/* FIXME -- set up public IP address/port for this FN */
	if on {
		fn.URLCall = true
	}
	return nil
}
