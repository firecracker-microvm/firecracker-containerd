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

package internal

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"

	"github.com/miekg/dns"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

const (
	domainName = "phony.domain.test"
)

// LocalNetworkServices provides a way of running bare-bones
// DNS and HTTP servers bound to the ip of a created device,
// which enables testing VM networking without relying on an
// external network.
type LocalNetworkServices interface {
	// DNSServerIP returns the IP address of the DNS server
	// (useful for seting resolv.conf)
	DNSServerIP() string

	// URL returns the full URL to use when querying the provided
	// subpath. i.e. if subpath is "foo/bar", this will return
	// something like "http://phony.domain.test/foo/bar"
	URL(subpath string) string

	// Serve will start the DNS and HTTP servers, blocking until
	// either of them shuts down.
	Serve(ctx context.Context) error
}

// NewLocalNetworkServices returns an implementation of
// LocalNetworkServices where the DNS server will bind to the
// ip of a created device and serve a single mapping from
// "domain" to that ip. The HTTP server will serve a pages at
// paths defined in the keys of "webpages", with the content set
// in the values of that map.
func NewLocalNetworkServices(webpages map[string]string) (LocalNetworkServices, error) {
	testDevName := "testdev0"
	ipAddr := "10.0.0.1"
	ipCidr := fmt.Sprintf("%s/32", ipAddr)

	output, err := exec.Command("ip", "tuntap", "add", testDevName, "mode", "tun").CombinedOutput()
	if err != nil {
		return nil, errors.Wrapf(err, `failed to add tun dev, "ip" command output: %s`, string(output))
	}

	output, err = exec.Command("ip", "addr", "add", ipCidr, "dev", testDevName).CombinedOutput()
	if err != nil {
		return nil, errors.Wrapf(err, `failed to assign ip to tun dev, "ip" command output: %s`, string(output))
	}

	output, err = exec.Command("ip", "link", "set", "dev", testDevName, "up").CombinedOutput()
	if err != nil {
		return nil, errors.Wrapf(err, `failed to set tun dev up, "ip" command output: %s`, string(output))
	}

	return &localNetworkServices{
		domain:   domainName,
		webpages: webpages,
		ipAddr:   ipAddr,
	}, nil
}

type localNetworkServices struct {
	domain   string
	webpages map[string]string
	ipAddr   string
}

func (l localNetworkServices) DNSServerIP() string {
	return l.ipAddr
}

func (l localNetworkServices) URL(subpath string) string {
	return fmt.Sprintf("http://%s/%s", l.domain, subpath)
}

func (l localNetworkServices) Serve(ctx context.Context) error {
	errGroup, _ := errgroup.WithContext(ctx)

	errGroup.Go(func() error {
		dnsSrv := &dns.Server{
			Addr: l.ipAddr + ":53",
			Net:  "udp",
			Handler: &dnsHandler{
				records: map[string]string{
					l.domain + ".": l.ipAddr,
				},
			},
		}
		return dnsSrv.ListenAndServe()
	})

	errGroup.Go(func() error {
		for path, contents := range l.webpages {
			webpage := contents
			http.HandleFunc("/"+path, func(w http.ResponseWriter, r *http.Request) {
				io.WriteString(w, webpage)
			})
		}

		return http.ListenAndServe(l.ipAddr+":80", nil)
	})

	return errGroup.Wait()
}

type dnsHandler struct {
	records map[string]string
}

func (h dnsHandler) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	msg := dns.Msg{}
	msg.SetReply(r)
	if r.Question[0].Qtype == dns.TypeA {
		msg.Authoritative = true
		domain := msg.Question[0].Name
		address, ok := h.records[domain]
		if ok {
			msg.Answer = append(msg.Answer, &dns.A{
				Hdr: dns.RR_Header{Name: domain, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 3600},
				A:   net.ParseIP(address),
			})
		} else {
			msg.SetRcode(r, dns.RcodeNameError)
		}
	}

	w.WriteMsg(&msg)
}
