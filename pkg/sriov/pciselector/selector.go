// Copyright (c) 2020 Doc.ai and/or its affiliates.
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at:
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package pciselector provides a PCI selector for endpoint-side SR-IOV connection
package pciselector

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/networkservicemesh/sdk/pkg/tools/log"

	"github.com/networkservicemesh/sdk-sriov/pkg/sriov"
)

// ResourcePool provides methods to select and free OS PCI functions
type ResourcePool interface {
	SelectAny(pfPciAddr string, driverType sriov.DriverType) (string, error)
	Free(vfPciAddr string)
}

// Selector is a PCI selector for endpoint-side SR-IOV connection
type Selector struct {
	ctx          context.Context
	resourcePool ResourcePool
	hosts        map[string]*sriov.HostInfo
	pfMappings   map[string]map[string]string
	lock         sync.Mutex
}

type selection struct {
	pfPciAddress string
	iommuGroup   uint
}

// NewSelector returns a new Selector
func NewSelector(ctx context.Context, resourcePool ResourcePool, config *Config) *Selector {
	s := &Selector{
		ctx:          ctx,
		resourcePool: resourcePool,
		hosts:        map[string]*sriov.HostInfo{},
		pfMappings:   config.Hosts,
	}

	for hostName := range config.Hosts {
		s.hosts[hostName] = &sriov.HostInfo{
			HostName: hostName,
		}
	}

	return s
}

// Update updates host info for the given host
func (s *Selector) Update(hostName string, host *sriov.HostInfo) error {
	mappings, ok := s.pfMappings[hostName]
	if !ok {
		return errors.New("trying to update info for not mapped host = " + hostName)
	}

	for pf := range host.PhysicalFunctions {
		if _, ok := mappings[pf]; !ok {
			return errors.New("trying to update info for not mapped PF = " + pf)
		}
	}

	s.lock.Lock()
	defer s.lock.Unlock()

	s.hosts[hostName] = host

	return nil
}

// Select selects a Client -> Endpoint functions pair for the given host, driver type and capability
func (s *Selector) Select(
	remoteHost *sriov.HostInfo,
	clientDriverType, endpointDriverType sriov.DriverType,
	capability sriov.Capability,
) (*Selection, error) {
	logEntry := log.Entry(s.ctx).WithField("Selector", "Select")

	s.lock.Lock()
	defer s.lock.Unlock()

	host, ok := s.hosts[remoteHost.HostName]
	if !ok {
		return nil, errors.New("invalid host = " + remoteHost.HostName)
	}

	selections := s.preSelect(host, clientDriverType, capability)
	if len(selections) == 0 {
		logEntry.Warnf("no PF found locally for: %v %v %v", host.HostName, clientDriverType, capability)
		// local info is probably outdated, let's try remote info
		selections = s.preSelect(remoteHost, clientDriverType, capability)
	}

	mappings := s.pfMappings[remoteHost.HostName]
	for _, selection := range selections {
		pfPciAddr := mappings[selection.pfPciAddress]
		vfPciAddr, err := s.resourcePool.SelectAny(pfPciAddr, endpointDriverType)
		if err != nil {
			continue
		}

		clientIg := host.PhysicalFunctions[selection.pfPciAddress].IommuGroups[selection.iommuGroup]
		clientIg.DriverType = clientDriverType
		if clientIg.FreeVirtualFunctions != 0 {
			clientIg.FreeVirtualFunctions--
		}
		s.updateIommuGroup(host, selection.iommuGroup)

		return &Selection{
			EndpointVfPciAddress: vfPciAddr,
			ClientPfPciAddress:   selection.pfPciAddress,
			ClientIommuGroup:     selection.iommuGroup,
		}, nil
	}

	return nil, errors.New("no virtual functions available")
}

func (s *Selector) preSelect(
	host *sriov.HostInfo,
	driverType sriov.DriverType,
	capability sriov.Capability,
) []*selection {
	var selections []*selection
	for pciAddr, pf := range host.PhysicalFunctions {
		if pf.Capability.Compare(capability) >= 0 {
			for igid, ig := range pf.IommuGroups {
				if ig.FreeVirtualFunctions > 0 && (ig.DriverType == sriov.NoDriver || ig.DriverType == driverType) {
					selections = append(selections, &selection{
						pfPciAddress: pciAddr,
						iommuGroup:   igid,
					})
				}
			}
		}
	}

	sort.Slice(selections, func(i, k int) bool {
		iPf := host.PhysicalFunctions[selections[i].pfPciAddress]
		iIg := iPf.IommuGroups[selections[i].iommuGroup]
		kPf := host.PhysicalFunctions[selections[k].pfPciAddress]
		kIg := kPf.IommuGroups[selections[k].iommuGroup]

		if iIg.DriverType == driverType && kIg.DriverType == sriov.NoDriver {
			return true
		}
		if iIg.DriverType == sriov.NoDriver && kIg.DriverType == driverType {
			return false
		}

		cmp := iPf.Capability.Compare(kPf.Capability)
		if cmp < 0 {
			return true
		}
		if cmp > 0 {
			return false
		}

		return (iIg.TotalVirtualFunctions - iIg.FreeVirtualFunctions) < (kIg.TotalVirtualFunctions - kIg.FreeVirtualFunctions)
	})

	return selections
}

func (s *Selector) updateIommuGroup(host *sriov.HostInfo, igid uint) {
	driverType := sriov.NoDriver
	for _, pf := range host.PhysicalFunctions {
		if ig, ok := pf.IommuGroups[igid]; ok && ig.FreeVirtualFunctions != ig.TotalVirtualFunctions {
			driverType = ig.DriverType
			break
		}
	}

	for _, pf := range host.PhysicalFunctions {
		if ig, ok := pf.IommuGroups[igid]; ok {
			ig.DriverType = driverType
		}
	}
}

// Free frees already selected Client -> Endpoint pair for the given host
func (s *Selector) Free(hostName string, selection *Selection) {
	logEntry := log.Entry(s.ctx).WithField("Selector", "Free")

	s.lock.Lock()
	defer s.lock.Unlock()

	s.resourcePool.Free(selection.EndpointVfPciAddress)

	host, ok := s.hosts[hostName]
	if !ok {
		logEntry.Warnf("trying to free PF for not existing host name: %v", hostName)
		return
	}

	pf, ok := host.PhysicalFunctions[selection.ClientPfPciAddress]
	if !ok {
		logEntry.Warnf("trying to free not mapped PF: %v", selection.ClientPfPciAddress)
		return
	}

	ig, ok := pf.IommuGroups[selection.ClientIommuGroup]
	if !ok {
		logEntry.Warnf("trying to free for not existing IOMMU group: %v", selection.ClientIommuGroup)
		return
	}

	if ig.FreeVirtualFunctions < ig.TotalVirtualFunctions {
		ig.FreeVirtualFunctions++
	}
	s.updateIommuGroup(host, selection.ClientIommuGroup)
}
