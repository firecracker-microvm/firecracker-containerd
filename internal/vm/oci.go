// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//	http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

/*
   Copyright The containerd Authors.
   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at
       http://www.apache.org/licenses/LICENSE-2.0
   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package vm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/containerd/containerd/mount"
	"github.com/containerd/containerd/oci"
	"github.com/containerd/continuity/fs"
	"github.com/opencontainers/runc/libcontainer/user"
	"github.com/opencontainers/runtime-spec/specs-go"
)

// UpdateUserInSpec modifies a serialized json spec object with user information from inside the container.
// If the client used firecrackeroci.WithVMLocalImageConfig or firecrackeroci.WithVMLocalUser, this
// method will do the mapping between username -> uid, group name -> gid, and lookup additional groups for the user.
// This is used to split where the user configures a spec via containerd's oci methods in the client
// from where the data is actually present (from the agent inside the VM)
func UpdateUserInSpec(ctx context.Context, specData []byte, rootfsMount mount.Mount) ([]byte, error) {
	var spec specs.Spec
	if err := json.Unmarshal(specData, &spec); err != nil {
		return specData, err
	}
	if err := updateUserInSpec(ctx, &spec, rootfsMount); err != nil {
		return specData, err
	}
	newSpecData, err := json.Marshal(&spec)
	if err != nil {
		return specData, err
	}
	return newSpecData, nil

}

func updateUserInSpec(ctx context.Context, s *specs.Spec, rootfsMount mount.Mount) error {
	userstr := s.Process.User.Username
	if userstr != "" {
		s.Process.User.Username = ""
		if err := withUser(ctx, s, rootfsMount, userstr); err != nil {
			return err
		}
		return withAdditionalGIDs(ctx, s, rootfsMount, userstr)
	}
	return withAdditionalGIDs(ctx, s, rootfsMount, "root")
}

// The remaining methods in this file are copies of https://github.com/containerd/containerd/blob/main/oci/spec_opts.go
// except that they operate directly on a spec rather than as functional arguments when creating a container.
// We do this so that we can run these methods from the agent running inside the VM rather than in the containerd client.
// These methods temporarily mount the container's rootfs to lookup information which is only possible inside the
// VM for VM local container rootfs'

// withUser sets the user to be used within the container.
// It accepts a valid user string in OCI Image Spec v1.0.0:
//   user, uid, user:group, uid:gid, uid:group, user:gid
func withUser(ctx context.Context, s *specs.Spec, rootfsMount mount.Mount, userstr string) error {
	parts := strings.Split(userstr, ":")
	switch len(parts) {
	case 1:
		v, err := strconv.Atoi(parts[0])
		if err != nil {
			// if we cannot parse as a uint they try to see if it is a username
			return withUsername(ctx, s, rootfsMount, parts[0])
		}
		return withUserID(ctx, s, rootfsMount, uint32(v))
	case 2:
		var (
			username  string
			groupname string
		)
		var uid, gid uint32
		v, err := strconv.Atoi(parts[0])
		if err != nil {
			username = parts[0]
		} else {
			uid = uint32(v)
		}
		if v, err = strconv.Atoi(parts[1]); err != nil {
			groupname = parts[1]
		} else {
			gid = uint32(v)
		}
		if username == "" && groupname == "" {
			s.Process.User.UID, s.Process.User.GID = uid, gid
			return nil
		}
		f := func(root string) error {
			if username != "" {
				user, err := userFromPath(root, func(u user.User) bool {
					return u.Name == username
				})
				if err != nil {
					return err
				}
				uid = uint32(user.Uid)
			}
			if groupname != "" {
				gid, err = gIDFromPath(root, func(g user.Group) bool {
					return g.Name == groupname
				})
				if err != nil {
					return err
				}
			}
			s.Process.User.UID, s.Process.User.GID = uid, gid
			return nil
		}
		return mount.WithTempMount(ctx, []mount.Mount{rootfsMount}, f)
	default:
		return fmt.Errorf("invalid USER value %s", userstr)
	}
}

// userFromPath inspects the user object using /etc/passwd in the specified rootfs.
// filter can be nil.
func userFromPath(root string, filter func(user.User) bool) (user.User, error) {
	ppath, err := fs.RootPath(root, "/etc/passwd")
	if err != nil {
		return user.User{}, err
	}
	users, err := user.ParsePasswdFileFilter(ppath, filter)
	if err != nil {
		return user.User{}, err
	}
	if len(users) == 0 {
		return user.User{}, oci.ErrNoUsersFound
	}
	return users[0], nil
}

// gIDFromPath inspects the GID using /etc/passwd in the specified rootfs.
// filter can be nil.
func gIDFromPath(root string, filter func(user.Group) bool) (gid uint32, err error) {
	gpath, err := fs.RootPath(root, "/etc/group")
	if err != nil {
		return 0, err
	}
	groups, err := user.ParseGroupFileFilter(gpath, filter)
	if err != nil {
		return 0, err
	}
	if len(groups) == 0 {
		return 0, oci.ErrNoGroupsFound
	}
	g := groups[0]
	return uint32(g.Gid), nil
}

// withUserID sets the correct UID and GID for the container based
// on the image's /etc/passwd contents. If /etc/passwd does not exist,
// or uid is not found in /etc/passwd, it sets the requested uid,
// additionally sets the gid to 0, and does not return an error.
func withUserID(ctx context.Context, s *specs.Spec, rootfsMount mount.Mount, uid uint32) error {
	return mount.WithTempMount(ctx, []mount.Mount{rootfsMount}, func(root string) error {
		user, err := userFromPath(root, func(u user.User) bool {
			return u.Uid == int(uid)
		})
		if err != nil {
			if os.IsNotExist(err) || err == oci.ErrNoUsersFound {
				s.Process.User.UID, s.Process.User.GID = uid, 0
				return nil
			}
			return err
		}
		s.Process.User.UID, s.Process.User.GID = uint32(user.Uid), uint32(user.Gid)
		return nil
	})
}

// withUsername sets the correct UID and GID for the container
// based on the image's /etc/passwd contents. If /etc/passwd
// does not exist, or the username is not found in /etc/passwd,
// it returns error.
func withUsername(ctx context.Context, s *specs.Spec, rootfsMount mount.Mount, username string) error {
	return mount.WithTempMount(ctx, []mount.Mount{rootfsMount}, func(root string) error {
		user, err := userFromPath(root, func(u user.User) bool {
			return u.Name == username
		})
		if err != nil {
			return err
		}
		s.Process.User.UID, s.Process.User.GID = uint32(user.Uid), uint32(user.Gid)
		return nil
	})
}

func withAdditionalGIDs(ctx context.Context, s *specs.Spec, rootfsMount mount.Mount, userstr string) error {
	setAdditionalGids := func(root string) error {
		var username string
		uid, err := strconv.Atoi(userstr)
		if err == nil {
			user, err := userFromPath(root, func(u user.User) bool {
				return u.Uid == uid
			})
			if err != nil {
				if os.IsNotExist(err) || err == oci.ErrNoUsersFound {
					return nil
				}
				return err
			}
			username = user.Name
		} else {
			username = userstr
		}
		gids, err := getSupplementalGroupsFromPath(root, func(g user.Group) bool {
			// we only want supplemental groups
			if g.Name == username {
				return false
			}
			for _, entry := range g.List {
				if entry == username {
					return true
				}
			}
			return false
		})
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		s.Process.User.AdditionalGids = gids
		return nil
	}
	return mount.WithTempMount(ctx, []mount.Mount{rootfsMount}, setAdditionalGids)
}

func getSupplementalGroupsFromPath(root string, filter func(user.Group) bool) ([]uint32, error) {
	gpath, err := fs.RootPath(root, "/etc/group")
	if err != nil {
		return []uint32{}, err
	}
	groups, err := user.ParseGroupFileFilter(gpath, filter)
	if err != nil {
		return []uint32{}, err
	}
	if len(groups) == 0 {
		// if there are no additional groups; just return an empty set
		return []uint32{}, nil
	}
	addlGids := []uint32{}
	for _, grp := range groups {
		addlGids = append(addlGids, uint32(grp.Gid))
	}
	return addlGids, nil
}
