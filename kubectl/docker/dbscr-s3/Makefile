swifty/dbscr-3:
	$(call msg-gen,$@)
	$(Q) docker build --network=host -t registry.gitlab.com/swiftyteam/$@ .
	$(Q) docker tag registry.gitlab.com/swiftyteam/$@ registry.gitlab.com/swiftyteam/$@:$(GITID)
	$(Q) docker tag registry.gitlab.com/swiftyteam/$@ registry.gitlab.com/swiftyteam/$@:$(SWIFTY_ENV)
	$(Q) docker push registry.gitlab.com/swiftyteam/$@
.PHONY: swifty/dbscr-3

all: swifty/dbscr-3
	@true
.PHONY: all
