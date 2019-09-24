# Copyright 2018-2019 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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
WORKDIR    := rootfs
WORKDIRLOC := $(shell readlink -f $(WORKDIR))
IMAGE_DIRS := /dev /bin /etc /etc/init.d /tmp /var /run /proc /sys /container/rootfs /agent /rom /overlay
DIRS       := $(foreach dir,$(IMAGE_DIRS),"$(WORKDIR)$(dir)")
DEBMIRROR  ?= http://deb.debian.org/debian

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
if [ -d $(subst _stamp,,$@) ]; then \
  cd $(subst _stamp,,$@) && tar cf - . | (cd "$(WORKDIRLOC)" && tar xvf -);\
fi
# Reset the timestamp on $(WORKDIR), which may have been modified by the
# creation of files in it:
touch --reference=debootstrap_stamp --no-create "$(WORKDIR)"
endef

all: rootfs.img

$(WORKDIR):
	mkdir $(WORKDIR)

image_files=$(shell find files_* -mindepth 1 -type f -print)
files_%_stamp: debootstrap_stamp $(image_files)
	$(install_dir)
	touch $@

debootstrap: debootstrap_stamp

debootstrap_stamp: $(WORKDIR)
ifneq ($(UID),0)
	$(error $(@) needs to run as root, not $(UID))
endif
	debootstrap \
		--variant=minbase \
		--include=udev,systemd,systemd-sysv,procps,libseccomp2 \
		stretch \
		"$(WORKDIR)" $(DEBMIRROR)
	rm -rf "$(WORKDIR)/var/cache/apt/archives" \
	       "$(WORKDIR)/usr/share/doc" \
	       "$(WORKDIR)/var/lib/apt/lists"
	mkdir -p $(DIRS)
	touch $@

rootfs.img: files_common_stamp files_debootstrap_stamp files_ephemeral_stamp
	mksquashfs "$(WORKDIR)" rootfs.img -noappend

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
		-it --volume $(CURDIR):/src \
		--cap-add=sys_admin \
		--cap-add=sys_chroot \
		--env=DEBMIRROR \
		fc-image-builder:$(DOCKER_IMAGE_TAG) $(subst -in-docker,,$@)

clean:
	-rm -f *stamp
	if [ -d $(WORKDIR) ] || [ -e rootfs.img ]; then \
	  if [ $(UID) -eq 0 ]; then \
	    rm -rf $(WORKDIR) \
	    rm -f rootfs.img ;\
	   else \
	     $(MAKE) clean-in-docker ;\
	   fi ; \
	fi

distclean: clean
	rm -rf files_ephemeral
	- docker rmi fc-image-builder:$(DOCKER_IMAGE_TAG)

.PHONY: debootstrap clean distclean builder %-in-docker
