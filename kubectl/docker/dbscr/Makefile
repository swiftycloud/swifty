swifty/swydbscr:
	$(call msg-gen,$@)
	$(Q) docker build --network=host -t $@ .
	$(Q) docker tag $@ $@:$(GITID)
.PHONY: swifty/swydbscr

all: swifty/swydbscr
	@true
.PHONY: all
