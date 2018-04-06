swifty/s3:
	$(call msg-gen,$@)
	$(Q) docker build --network=host -t $@ .
	$(Q) docker tag $@ $@:$(GITID)
.PHONY: swifty/s3

all: swifty/s3
	@true
.PHONY: all
