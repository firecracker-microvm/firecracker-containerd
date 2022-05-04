# Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License"). You may
# not use this file except in compliance with the License. A copy of the
# License is located at
#
#       http://aws.amazon.com/apache2.0/
#
# or in the "license" file accompanying this file. This file is distributed
# on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
# express or implied. See the License for the specific language governing
# permissions and limitations under the License.

UID        := $(shell id -u)
WORKDIR    := tmp/rootfs
WORKDIRLOC := $(shell readlink -f $(WORKDIR))
IMAGE_DIRS := /dev /bin /etc /etc/init.d /tmp /var /run /proc /sys /container/rootfs /agent /rom /overlay
DIRS       := $(foreach dir,$(IMAGE_DIRS),"$(WORKDIR)$(dir)")
DEBMIRROR  ?= http://deb.debian.org/debian
PACKAGES   := udev systemd systemd-sysv procps libseccomp2 haveged

COMMA      := ,
EMPTY      :=
SPACE      := $(EMPTY) $(EMPTY)

export DOCKER_IMAGE_TAG?=latest

# When referenced in a _stamp target's recipe,
# e.g. files_debootstrap_stamp, this function uses tar to copy the
# contents of the base directory (e.g. files_debootstrap) to the image
# root. In order for the file locations to be correctly represented in
# the image root, the directory structure of the source directory
# should match that of the root image, e.g. if files_debootstrap
# contains usr/local/bin/foo, then 'foo' will be installed in
# /usr/local/bin/ in the root filesystem.
define install_dir =
if [ -d $(subst $(SUFFIX),,$@) ]; then \
  cd $(subst $(SUFFIX),,$@) && tar cf - . | (cd "$(WORKDIRLOC)" && tar xvf -);\
fi
# Reset the timestamp on $(WORKDIR), which may have been modified by the
# creation of files in it:
touch --reference=debootstrap$(SUFFIX) --no-create "$(WORKDIR)"
endef

define run_debootstrap =
	debootstrap \
		--variant=minbase \
		--include=$(subst $(SPACE),$(COMMA),$(PACKAGES))\
		bookworm \
		"$(WORKDIR)" $(DEBMIRROR)
	rm -rf "$(WORKDIR)/var/cache/apt/archives" \
	       "$(WORKDIR)/usr/share/doc" \
	       "$(WORKDIR)/var/lib/apt/lists"
	mkdir -p $(DIRS)
	touch $@
endef

all: rootfs.img rootfs-slow-boot.img rootfs-slow-reboot.img rootfs-no-agent.img
stargz: rootfs-stargz.img

$(WORKDIR):
	mkdir $(WORKDIR)

image_files=$(shell find files_* -mindepth 1 -type f -print)
files_%_stamp: SUFFIX := _stamp
files_%_stamp: debootstrap_stamp $(image_files)
	$(install_dir)
	touch $@

files_%_sgzstamp: SUFFIX := _sgzstamp
files_%_sgzstamp: debootstrap_sgzstamp $(image_files)
	$(install_dir)
	touch $@

debootstrap: debootstrap_stamp

debootstrap_stamp: $(WORKDIR)
ifneq ($(UID),0)
	$(error $(@) needs to run as root, not $(UID))
endif
	$(run_debootstrap)

debootstrap_%stamp: $(WORKDIR)
ifneq ($(UID),0)
	$(error $(@) needs to run as root, not $(UID))
endif
	$(run_debootstrap)

rootfs.img: files_common_stamp files_debootstrap_stamp files_ephemeral_stamp
	mksquashfs "$(WORKDIR)" rootfs.img -noappend

# Intentionally break the rootfs to simulate the case where CreateVM is taking longer than the minimal timeout
rootfs-slow-boot.img: files_common_stamp files_debootstrap_stamp files_ephemeral_stamp
	rm -fr tmp/$@
	cp -a "$(WORKDIR)" tmp/$@
	echo 'ExecStartPre=/bin/sleep 30' >> tmp/$@/etc/systemd/system/firecracker-agent.service
	mksquashfs tmp/$@ $@ -noappend

# Intentionally break the rootfs to simulate the case where StopVM is taking longer than the minimal timeout
rootfs-slow-reboot.img: files_common_stamp files_debootstrap_stamp files_ephemeral_stamp
	rm -fr tmp/$@
	cp -a "$(WORKDIR)" tmp/$@
	echo 'ExecStop=/bin/sleep 60' >> tmp/$@/etc/systemd/system/firecracker-agent.service
	mksquashfs tmp/$@ $@ -noappend

rootfs-no-agent.img: files_common_stamp files_debootstrap_stamp files_ephemeral_stamp
	rm -fr tmp/$@
	cp -a "$(WORKDIR)" tmp/$@
	rm tmp/$@/etc/systemd/system/firecracker-agent.service
	rm tmp/$@/usr/local/bin/agent
	mksquashfs tmp/$@ $@ -noappend

rootfs-stargz.img: WORKDIR    := $(addsuffix -stargz, $(WORKDIR))
rootfs-stargz.img: WORKDIRLOC := $(shell readlink -f $(WORKDIR))
rootfs-stargz.img: DIRS       := $(foreach dir,$(IMAGE_DIRS),"$(WORKDIR)$(dir)")
rootfs-stargz.img: PACKAGES   += fuse ca-certificates socat
rootfs-stargz.img: files_common_sgzstamp files_debootstrap_sgzstamp files_ephemeral_sgzstamp files_stargz_sgzstamp files_ephemeral_stargz_sgzstamp
	mksquashfs "$(WORKDIR)" $@ -noappend

builder: builder_stamp

builder_stamp:
	docker build -t fc-image-builder:$(DOCKER_IMAGE_TAG) -f Dockerfile.debian-image .
	touch $@

# For any given target, append "-in-docker" to it to run the build
# recipe in a container, e.g. instead of:
# $ make rootfs.img
# you can use
# $ make rootfs.img-in-docker
%-in-docker: builder_stamp
	docker run --rm \
		--security-opt=apparmor=unconfined \
		--volume $(CURDIR):/src \
		--volume /src/tmp \
		--cap-add=sys_admin \
		--cap-add=sys_chroot \
		--env=DEBMIRROR \
		fc-image-builder:$(DOCKER_IMAGE_TAG) $(subst -in-docker,,$@)

clean:
	-rm -f *stamp
	if [ $(UID) -eq 0 ]; then \
	  rm -f rootfs.img rootfs-slow-boot.img rootfs-slow-reboot.img rootfs-stargz.img;\
	else \
	  $(MAKE) clean-in-docker ;\
	fi

distclean: clean
	rm -rf files_ephemeral
	- docker rmi fc-image-builder:$(DOCKER_IMAGE_TAG)

.PHONY: debootstrap clean distclean builder %-in-docker
