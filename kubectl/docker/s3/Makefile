include ../Makefile.inc

swifty/s3:
	$(call msg-gen,$@)
	$(Q) docker build --network=host -t $(IMG_PREFIX)$@ .
	$(Q) docker tag $(IMG_PREFIX)$@ $(IMG_PREFIX)$@:$(GITID)
	$(Q) docker tag $(IMG_PREFIX)$@ $(IMG_PREFIX)$@:$(SWIFTY_ENV)
	$(Q) docker push $(IMG_PREFIX)$@
.PHONY: swifty/s3

all: swifty/s3
	@true
.PHONY: all
