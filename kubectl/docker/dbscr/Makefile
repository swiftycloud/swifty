swifty/dbscr:
	$(call msg-gen,$@)
	$(Q) docker build --network=host -t $@ .
	$(Q) docker tag $@ $@:$(GITID)
.PHONY: swifty/dbscr

all: swifty/dbscr
	@true
.PHONY: all
