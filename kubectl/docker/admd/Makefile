swifty/admd:
	$(call msg-gen,$@)
	$(Q) docker build --network=host -t $@ .
	$(Q) docker tag $@ $@:$(GITID)
.PHONY: swifty/admd

all: swifty/admd
	@true
.PHONY: all
