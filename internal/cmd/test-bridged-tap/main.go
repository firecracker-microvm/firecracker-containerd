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
This is a bare-bones CNI plugin (with only ADD actually implemented, other
methods are no-ops) used to setup a network bridge w/ tap in order to
compare performance of a bridge-based VM network w/ a TC-based approach,
such as that created by the tc-redirect-tap plugin. It expects to be chained
after the ptp plugin and sets up a layout like the following:
[host] <- vethA <- vethB <- [bridge] <- tap0 <- [vm guest]

with "vethA <- vethB" crossing a network namespace boundary.
*/
package main

import (
	"encoding/json"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/containernetworking/plugins/pkg/utils/buildversion"
	"github.com/pkg/errors"
	"github.com/vishvananda/netlink"
)

func main() {
	skel.PluginMain(add, check, del,
		// support CNI versions that support plugin chaining
		version.PluginSupports("0.3.0", "0.3.1", version.Current()),
		buildversion.BuildString("test-bridged-tap"),
	)
}

func add(args *skel.CmdArgs) error {
	const mtu = 1500
	vethName := args.IfName

	return ns.WithNetNSPath(args.Netns, func(_ ns.NetNS) error {
		currentResult, err := getCurrentResult(args)
		if err != nil {
			return err
		}

		vethLink, err := netlink.LinkByName(vethName)
		if err != nil {
			return err
		}

		var vethIPConf *current.IPConfig
		for _, ipConfig := range currentResult.IPs {
			if ifaceIndex := ipConfig.Interface; ifaceIndex != nil && *ifaceIndex == 1 {
				vethIPConf = ipConfig
				break
			}
		}
		if vethIPConf == nil {
			return errors.Errorf("did not find veth ip: %+v", currentResult)
		}

		err = netlink.AddrDel(vethLink, &netlink.Addr{
			IPNet: &vethIPConf.Address,
		})
		if err != nil {
			return err
		}

		tapName := "tap0"
		err = netlink.LinkAdd(&netlink.Tuntap{
			Mode:   netlink.TUNTAP_MODE_TAP,
			Queues: 1,
			Flags:  netlink.TUNTAP_ONE_QUEUE | netlink.TUNTAP_VNET_HDR,
			LinkAttrs: netlink.LinkAttrs{
				Name: tapName,
			},
		})
		if err != nil {
			return err
		}

		tapLink, err := netlink.LinkByName(tapName)
		if err != nil {
			return err
		}

		err = netlink.LinkSetMTU(tapLink, mtu)
		if err != nil {
			return err
		}

		err = netlink.LinkSetUp(tapLink)
		if err != nil {
			return err
		}

		bridgeName := "br0"
		err = netlink.LinkAdd(&netlink.Bridge{
			LinkAttrs: netlink.LinkAttrs{
				Name:         bridgeName,
				MTU:          mtu,
				HardwareAddr: vethLink.Attrs().HardwareAddr,
			},
		})
		if err != nil {
			return err
		}

		bridgeLink, err := netlink.LinkByName(bridgeName)
		if err != nil {
			return err
		}

		err = netlink.LinkSetMaster(vethLink, bridgeLink.(*netlink.Bridge))
		if err != nil {
			return err
		}

		err = netlink.LinkSetMaster(tapLink, bridgeLink.(*netlink.Bridge))
		if err != nil {
			return err
		}

		err = netlink.LinkSetUp(bridgeLink)
		if err != nil {
			return err
		}

		currentResult.Interfaces = append(currentResult.Interfaces, &current.Interface{
			Name:    tapLink.Attrs().Name,
			Sandbox: args.Netns,
			Mac:     tapLink.Attrs().HardwareAddr.String(),
		})

		tapMac := tapLink.Attrs().HardwareAddr
		tapMac[1], tapMac[2] = tapMac[2], tapMac[1] // just need a different addr from the tap
		currentResult.Interfaces = append(currentResult.Interfaces, &current.Interface{
			Name:    tapLink.Attrs().Name,
			Sandbox: args.ContainerID,
			Mac:     tapMac.String(),
		})
		vmIfaceIndex := len(currentResult.Interfaces) - 1

		currentResult.IPs = append(currentResult.IPs, &current.IPConfig{
			Address:   vethIPConf.Address,
			Gateway:   vethIPConf.Gateway,
			Interface: &vmIfaceIndex,
		})

		return types.PrintResult(currentResult, currentResult.CNIVersion)
	})
}

func getCurrentResult(args *skel.CmdArgs) (*current.Result, error) {
	// parse the previous CNI result (or throw an error if there wasn't one)
	cniConf := types.NetConf{}
	err := json.Unmarshal(args.StdinData, &cniConf)
	if err != nil {
		return nil, errors.Wrap(err, "failure checking for previous result output")
	}

	err = version.ParsePrevResult(&cniConf)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse previous CNI result")
	}

	if cniConf.PrevResult == nil {
		return nil, errors.New("no previous result")
	}

	currentResult, err := current.NewResultFromResult(cniConf.PrevResult)
	if err != nil {
		return nil, errors.Wrap(err,
			"failed to generate current result from previous CNI result")
	}

	return currentResult, nil
}

func del(args *skel.CmdArgs) error {
	return nil
}

func check(args *skel.CmdArgs) error {
	return nil
}
