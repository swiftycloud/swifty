swifty/admd:
	$(call msg-gen,$@)
	$(Q) docker build -t $@ .
	$(Q) docker tag $@ $@:$(GITID)
.PHONY: swifty/admd

all: swifty/admd
	@true
.PHONY: all
