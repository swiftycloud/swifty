.PHONY: all .FORCE
.DEFAULT_GOAL := all

ifeq ($(strip $(V)),)
        E := @echo
        Q := @
else
        E := @\#
        Q :=
endif

export E Q

define msg-gen
        $(E) "  GEN     " $(1)
endef

define msg-clean
        $(E) "  CLEAN   " $(1)
endef

export msg-gen msg-clean

MAKEFLAGS += --no-print-directory
export MAKEFLAGS

RM		?= rm -f
MAKE		?= make
GIT		?= git
CP		?= cp -f
GO		?= go
GO-BUILD-OPTS	?= build
GOTAGS		?= gotags
MONGO		?= mongo --quiet localhost:27017
KUBECTL		?= kubectl
IPVSADM		?= ipvsadm

LOCAL_SOURCES	?= /home/swifty/local-sources
VOLUME_DIR	?= /home/swifty-volume
TEST_REPO	?= test/.repo

export RM MAKE GIT CP GO GO-BUILD-OPTS GOTAGS MONGO KUBECTL IPVSADM

# Build daemon
define gen-gobuild
swy-$(1): $$(go-$(1)-y) .FORCE
	$$(call msg-gen,$$@)
	$$(Q) $$(GO) $$(GO-BUILD-OPTS) -o $$@ $$(go-$(1)-y)
all-y += swy-$(1)
endef

# Build tool
define gen-gobuild-t
swy$(1): $$(go-$(1)-y) .FORCE
	$$(call msg-gen,$$@)
	$$(Q) $$(GO) $$(GO-BUILD-OPTS) -o $$@ $$(go-$(1)-y)
all-y += swy$(1)
endef

go-gate-y	+= src/gate/db.go
go-gate-y	+= src/gate/k8s.go
go-gate-y	+= src/gate/function.go
go-gate-y	+= src/gate/mware.go
go-gate-y	+= src/gate/mw-maria.go
go-gate-y	+= src/gate/mw-postgres.go
go-gate-y	+= src/gate/mw-rabbit.go
go-gate-y	+= src/gate/mw-mongo.go
go-gate-y	+= src/gate/mw-s3.go
go-gate-y	+= src/gate/balancer.go
go-gate-y	+= src/gate/main.go
go-gate-y	+= src/gate/runner.go
go-gate-y	+= src/gate/mq.go
go-gate-y	+= src/gate/event.go
go-gate-y	+= src/gate/runtime.go
go-gate-y	+= src/gate/repo.go
go-gate-y	+= src/gate/funcurl.go
go-gate-y	+= src/gate/stats.go
go-gate-y	+= src/gate/swoid.go

go-admd-y	+= src/admd/main.go
go-admd-y	+= src/admd/ks.go

go-pgrest-y	+= src/pgrest/main.go

go-s3-y	+= src/s3/main.go
go-s3-y	+= src/s3/db.go
go-s3-y	+= src/s3/bucket.go
go-s3-y	+= src/s3/object.go
go-s3-y	+= src/s3/s3.go
go-s3-y	+= src/s3/error.go
go-s3-y	+= src/s3/sign.go
go-s3-y	+= src/s3/resp.go
go-s3-y	+= src/s3/keys.go
go-s3-y	+= src/s3/rados.go
go-s3-y	+= src/s3/helpers.go

go-ctl-y	+= src/tools/ctl.go
go-sg-y		+= src/tools/sg.go

$(eval $(call gen-gobuild,gate))
$(eval $(call gen-gobuild,admd))
$(eval $(call gen-gobuild,pgrest))
$(eval $(call gen-gobuild,s3))
$(eval $(call gen-gobuild-t,ctl))
$(eval $(call gen-gobuild-t,sg))

# Default target
all: $(all-y)

#
# Docker images
swifty/python: src/wdog/main.py kubectl/docker/images/python/Dockerfile
	$(call msg-gen,$@)
	$(Q) $(CP) src/wdog/main.py  kubectl/docker/images/python/swy-wdog
	$(Q) $(MAKE) -C kubectl/docker/images/python all
.PHONY: swifty/python

swifty/golang: swy-wdog kubectl/docker/images/golang/Dockerfile
	$(call msg-gen,$@)
	$(Q) $(CP) swy-wdog  kubectl/docker/images/golang/
	$(Q) $(MAKE) -C kubectl/docker/images/golang all
.PHONY: swifty/golang

swifty/swift: swy-wdog kubectl/docker/images/swift/Dockerfile
	$(call msg-gen,$@)
	$(Q) $(CP) swy-wdog  kubectl/docker/images/swift/
	$(Q) $(MAKE) -C kubectl/docker/images/swift all
.PHONY: swifty/swift

swifty/nodejs: swy-wdog kubectl/docker/images/nodejs/Dockerfile
	$(call msg-gen,$@)
	$(Q) $(CP) swy-wdog  kubectl/docker/images/nodejs/
	$(Q) $(MAKE) -C kubectl/docker/images/nodejs all
.PHONY: swifty/nodejs

images: swifty/python swifty/golang swifty/swift swifty/nodejs
	@true
.PHONY: images

help:
	@echo '    Targets:'
	@echo '      all             - Build all [*] targets'
	@echo '      images          - Build all docker images'
	@echo '      docs            - Build documentation'
	@echo '    * swy-gate        - Build gate'
	@echo '    * swy-wdog        - Build watchdog'
	@echo '    * swy-admd        - Build adm daemon'
	@echo '    * swy-s3          - Build s3 daemon'
	@echo '      swifty/python   - Build swifty/python docker image'
	@echo '      swifty/golang   - Build swifty/golang docker image'
	@echo '      swifty/swift    - Build swifty/swift docker image'
	@echo '      swifty/nodejs   - Build swifty/nodejs docker image'
	@echo '      rsclean         - Cleanup resources'
	@echo '      mqclean         - Cleanup rabbitmq'
	@echo '      sqlclean        - Cleanup mariadb'
.PHONY: help

tags:
	$(Q) $(GOTAGS) -R src/ > tags
.PHONY: tags

docs: .FORCE
	$(Q) $(MAKE) -C docs all
.PHONY: docs

tarball:
	$(Q) $(GIT) archive --format=tar --prefix=swifty/ HEAD > swifty.tar
.PHONY: tarball

ifneq ($(filter mqclean,$(MAKECMDGOALS)),)
rabbit-users := $(filter-out guest root, $(shell rabbitmqctl list_users | tail -n +2 | cut -f 1))
rabbit-vhosts := $(filter-out /, $(shell rabbitmqctl list_vhosts | tail -n +2 | cut -f 1))
endif

mqclean: .FORCE
	$(call msg-gen,"Cleaning up MessageQ")
ifneq ($(rabbit-users),)
	$(Q) rabbitmqctl delete_user $(rabbit-users)
endif
ifneq ($(rabbit-vhosts),)
	$(Q) rabbitmqctl delete_vhost $(rabbit-vhosts)
endif
.PHONY: mqclean

ifneq ($(filter sqlclean,$(MAKECMDGOALS)),)
mysql-user ?= "root"
mysql-pass ?= "aiNe1sah9ichu1re"
sql-users := $(filter-out root, \
	$(shell mysql -u$(mysql-user) -p$(mysql-pass) -N -e'select user from mysql.user' | cut -f1))
sql-dbases := $(filter-out information_schema mysql performance_schema test, \
	$(shell mysql -u$(mysql-user) -p$(mysql-pass) -N -e'show databases' | cut -f1))
endif

sqlclean: .FORCE
	$(call msg-gen,"Cleaning up SQL")
ifneq ($(sql-users),)
	$(foreach user,$(sql-users),$(shell mysql -u$(mysql-user) -p$(mysql-pass) -e'drop user $(user)'))
endif
ifneq ($(sql-dbases),)
	$(foreach db,$(sql-dbases),$(shell mysql -u$(mysql-user) -p$(mysql-pass) -e'drop database $(db)'))
endif
.PHONY: sqlclean

DB-SWIFTY	:= swifty
DB-S3		:= swifty-s3

clean-db-swifty:
	$(call msg-gen,"Cleaning up main MongoDB")
	$(Q) $(MONGO)/$(DB-SWIFTY) --eval 'db.Function.remove({});'
	$(Q) $(MONGO)/$(DB-SWIFTY) --eval 'db.Mware.remove({});'
	$(Q) $(MONGO)/$(DB-SWIFTY) --eval 'db.Pods.remove({});'
	$(Q) $(MONGO)/$(DB-SWIFTY) --eval 'db.Balancer.remove({});'
	$(Q) $(MONGO)/$(DB-SWIFTY) --eval 'db.BalancerRS.remove({});'
	#$(Q) $(MONGO)/$(DB-SWIFTY) --eval 'db.Logs.remove({});'
.PHONY: clean-db-swifty

clean-db-s3:
	$(call msg-gen,"Cleaning up s3 MongoDB")
	$(Q) $(MONGO)/$(DB-S3) --eval 'db.S3Buckets.remove({});'
	$(Q) $(MONGO)/$(DB-S3) --eval 'db.S3Objects.remove({});'
	$(Q) $(MONGO)/$(DB-S3) --eval 'db.S3ObjectData.remove({});'
	#$(Q) $(MONGO)/$(DB-S3) --eval 'db.S3AccessKeys.remove({});'
.PHONY: clean-db-s3

rsclean: clean-db-swifty clean-db-s3
	$(call msg-gen,"Cleaning up kubernetes")
	$(Q) $(KUBECTL) delete deployment --all
	$(Q) $(KUBECTL) delete secret --all
	#$(Q) $(KUBECTL) delete service --all
	$(Q) $(KUBECTL) delete pod --all
	$(call msg-gen,"Cleaning up IPVS")
	$(Q) $(IPVSADM) -C
	$(call msg-gen,"Cleaning up FS")
ifneq ($(wildcard $(LOCAL_SOURCES)/.*),)
	$(Q) $(RM) -r $(LOCAL_SOURCES)/*
endif
ifneq ($(wildcard $(VOLUME_DIR)/.*),)
	$(Q) $(RM) -r $(VOLUME_DIR)/*
endif
ifneq ($(wildcard $(TEST_REPO)/.*),)
	$(Q) $(RM) -r $(TEST_REPO)/*
endif

clean:
	$(call msg-clean,swy-gate)
	$(Q) $(RM) swy-gate
	$(call msg-clean,swy-wdog)
	$(Q) $(RM) swy-wdog
	$(call msg-clean,swy-admd)
	$(Q) $(RM) swy-admd
	$(call msg-clean,swy-s3)
	$(Q) $(RM) swy-s3
	$(Q) $(MAKE) -C docs clean
.PHONY: clean

mrproper: clean
	$(call msg-clean,tags)
	$(Q) $(RM) tags
.PHONY: mrproper

.SUFFIXES:
