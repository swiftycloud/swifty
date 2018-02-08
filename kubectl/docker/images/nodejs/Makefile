swifty/nodejs:
	$(call msg-gen,$@)
	$(Q) docker build -t $@ .
	$(Q) docker tag $@ $(GITID)
.PHONY: swifty/nodejs

all: swifty/nodejs
	@true
.PHONY: all
