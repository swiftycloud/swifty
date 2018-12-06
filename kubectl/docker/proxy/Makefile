swifty/proxy:
	$(call msg-gen,$@)
	$(Q) docker build --network=host -t registry.gitlab.com/swiftyteam/$@ .
	$(Q) docker tag registry.gitlab.com/swiftyteam/$@ registry.gitlab.com/swiftyteam/$@:$(GITID)
	$(Q) docker tag registry.gitlab.com/swiftyteam/$@ registry.gitlab.com/swiftyteam/$@:$(SWIFTY_ENV)
	$(Q) docker push registry.gitlab.com/swiftyteam/$@
.PHONY: swifty/proxy

all: swifty/proxy
	@true
.PHONY: all
